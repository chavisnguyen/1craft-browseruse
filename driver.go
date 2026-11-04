package main

import (
	"context"
	"fmt"
	"strings"
)

// PageSummary contains basic metadata and extracted interactive elements of a page.
type PageSummary struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Elements string `json:"elements"`
	Count    int    `json:"count,omitempty"`
}

// TabInfo represents high-level info about an open tab.
type TabInfo struct {
	ID      string `json:"tab_id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Profile string `json:"profile,omitempty"`
	Active  bool   `json:"active,omitempty"`
	GroupID *int   `json:"group_id,omitempty"`
}

// Driver defines the primitives supported by a browser backend (extension bridge or headless fallback).
type Driver interface {
	Name() string
	Mode() string
	CheckAlive(ctx context.Context, handle string) bool
	NewTab(ctx context.Context, url string) (handle string, err error)
	ListTabs(ctx context.Context, handles []string) ([]TabInfo, error)
	CloseTab(ctx context.Context, handle string) error
	Navigate(ctx context.Context, handle, url, action string) error
	ReadPage(ctx context.Context, handle, filter string) (PageSummary, error)
	Computer(ctx context.Context, handle, action string, params map[string]any) (any, error)
	FormInput(ctx context.Context, handle, refID string, value any, checked *bool) (any, error)
	Find(ctx context.Context, handle, query string) (any, error)
	ReadNetworkRequests(ctx context.Context, handle string, clear bool, filter string) (any, error)
	ReadConsoleMessages(ctx context.Context, handle string, clear bool) ([]string, error)
	UploadFile(ctx context.Context, handle, selector string, files []string) error
	Evaluate(ctx context.Context, handle, script string) (any, error)
	BrowserBatch(ctx context.Context, handle string, actions []any) (any, error)
	Screenshot(ctx context.Context, handle string, annotate bool) ([]byte, error)
	Close() error

	// Compatibility helpers
	Snapshot(ctx context.Context, handle string) (PageSummary, error)
	Click(ctx context.Context, handle, selector string) error
	Hover(ctx context.Context, handle, selector string) error
	Type(ctx context.Context, handle, selector, text string) error
	PressKey(ctx context.Context, handle, key string) error
	Select(ctx context.Context, handle, selector, value string) error
	WaitFor(ctx context.Context, handle, selector string, timeoutMs int, state string) error
	DragAndDrop(ctx context.Context, handle, fromSelector, toSelector string) error
	Scroll(ctx context.Context, handle, direction string, amount int) error
	ActivateTab(ctx context.Context, handle string) error
	ConsoleLogs(ctx context.Context, handle string, clear bool) ([]string, error)
}

func normalizeURL(u string) string {
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") ||
		strings.HasPrefix(u, "data:") || strings.HasPrefix(u, "file://") ||
		strings.HasPrefix(u, "about:") {
		return u
	}
	return "https://" + u
}

func formatPageSummary(tabID string, summary PageSummary) string {
	return fmt.Sprintf("**Tab ID**: `%s`\n**Current URL**: `%s`\n**Page Title**: %s\n\n**Accessibility Tree Elements** (target with `@e1`, `@e2` or selector):\n```\n%s\n```",
		tabID, summary.URL, summary.Title, summary.Elements)
}

func formatPageSummaryCompact(tabID string, summary PageSummary) string {
	return fmt.Sprintf("**Tab ID**: `%s` | **Current URL**: `%s` | **Page Title**: %s",
		tabID, summary.URL, summary.Title)
}

// jsExtractElements for headless rod fallback with password masking & rich ARIA roles
const jsExtractElements = `() => {
    const oldRefs = document.querySelectorAll('[data-browseruse-ref]');
    for (const el of oldRefs) {
        el.removeAttribute('data-browseruse-ref');
    }

    const items = [];
    const elements = document.querySelectorAll('a, button, input, textarea, select, [role="button"], [role="link"], [role="checkbox"], [role="radio"], [role="tab"], [role="menuitem"], h1, h2, h3');
    let refCount = 1;

    function isSensitive(el) {
        const type = (el.getAttribute('type') || '').toLowerCase();
        if (type === 'password' || type === 'hidden') return true;
        const auto = (el.getAttribute('autocomplete') || '').toLowerCase();
        if (auto.includes('password') || auto.includes('one-time-code') || auto.includes('cc-')) return true;
        return false;
    }

    for (const el of elements) {
        const rect = el.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) continue;
        const style = window.getComputedStyle(el);
        if (style.display === 'none' || style.visibility === 'hidden') continue;

        let text = (el.innerText || el.value || el.placeholder || el.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ');
        if (isSensitive(el)) {
            text = '[REDACTED]';
        }

        const tag = el.tagName.toLowerCase();
        const role = el.getAttribute('role') || (tag === 'a' ? 'link' : tag === 'input' ? (el.type || 'textbox') : tag);
        if (text || tag === 'input' || tag === 'textarea' || tag === 'select') {
            const ref = 'e' + refCount;
            el.setAttribute('data-browseruse-ref', ref);
            refCount++;

            let meta = [];
            if (el.id) meta.push('id="' + el.id + '"');
            if (el.name) meta.push('name="' + el.name + '"');
            if (el.type) meta.push('type="' + el.type + '"');
            if (el.placeholder && el.placeholder !== text && text !== '[REDACTED]') meta.push('placeholder="' + el.placeholder + '"');
            const metaStr = meta.length > 0 ? ' (' + meta.join(' ') + ')' : '';
            const truncText = text.length > 80 ? text.substring(0, 77) + '...' : text;
            items.push('@' + ref + ' [' + role + '] "' + truncText + '"' + metaStr);
        }
    }
    return items.join('\n');
}`

const jsShowVirtualCursor = `(targetX, targetY, isClick) => {
    let styleEl = document.getElementById('browseruse-cursor-styles');
    if (!styleEl) {
        styleEl = document.createElement('style');
        styleEl.id = 'browseruse-cursor-styles';
        styleEl.textContent = ` + "`" + `
            @keyframes browseruse-click-ripple {
                0% { transform: scale(0.2); opacity: 1; border-width: 4px; }
                50% { opacity: 0.85; }
                100% { transform: scale(3.0); opacity: 0; border-width: 1px; }
            }
            .browseruse-ripple-effect {
                position: fixed !important;
                width: 44px !important;
                height: 44px !important;
                margin-left: -22px !important;
                margin-top: -22px !important;
                border-radius: 50% !important;
                border: 3px solid #ff2a2a !important;
                background: radial-gradient(circle, rgba(255, 42, 42, 0.45) 0%, rgba(255, 42, 42, 0) 70%) !important;
                pointer-events: none !important;
                z-index: 2147483646 !important;
                animation: browseruse-click-ripple 0.6s cubic-bezier(0.1, 0.8, 0.3, 1) forwards !important;
            }
        ` + "`" + `;
        document.documentElement.appendChild(styleEl);
    }

    let cursor = document.getElementById('browseruse-virtual-cursor');
    if (!cursor) {
        cursor = document.createElement('div');
        cursor.id = 'browseruse-virtual-cursor';
        cursor.style.cssText = ` + "`" + `
            position: fixed !important;
            top: 0 !important;
            left: 0 !important;
            margin-left: -4px !important;
            margin-top: -2px !important;
            z-index: 2147483647 !important;
            pointer-events: none !important;
            user-select: none !important;
            will-change: transform, opacity !important;
            transition: opacity 0.3s ease, transform 0.45s cubic-bezier(0.25, 1, 0.5, 1) !important;
            opacity: 0 !important;
        ` + "`" + `;
        cursor.innerHTML = ` + "`" + `
            <svg width="28" height="28" viewBox="0 0 32 32" fill="none" style="filter: drop-shadow(0 0 6px rgba(255, 42, 42, 0.85)) drop-shadow(0 2px 5px rgba(0,0,0,0.5)); transform-origin: 5px 3px; transition: transform 0.12s ease-out;">
                <path d="M5 3L14 26L18 17L27 13L5 3Z" fill="#ff2a2a" stroke="#ffffff" stroke-width="2.5" stroke-linejoin="round"/>
                <circle cx="6" cy="4" r="3" fill="#ffffff" />
            </svg>
        ` + "`" + `;
        const startX = window.__browseruse_last_x !== undefined ? window.__browseruse_last_x : Math.round(window.innerWidth / 2);
        const startY = window.__browseruse_last_y !== undefined ? window.__browseruse_last_y : Math.round(window.innerHeight / 2);
        cursor.style.setProperty('transform', 'translate3d(' + startX + 'px, ' + startY + 'px, 0)', 'important');
        document.documentElement.appendChild(cursor);
        void cursor.offsetWidth;
    }

    if (window.__browseruse_fade_timer) {
        clearTimeout(window.__browseruse_fade_timer);
    }
    cursor.style.setProperty('opacity', '1', 'important');
    cursor.style.setProperty('transform', 'translate3d(' + targetX + 'px, ' + targetY + 'px, 0)', 'important');
    window.__browseruse_last_x = targetX;
    window.__browseruse_last_y = targetY;

    if (isClick) {
        setTimeout(() => {
            const svg = cursor.querySelector('svg');
            if (svg) svg.style.transform = 'scale(0.82)';

            const ripple = document.createElement('div');
            ripple.className = 'browseruse-ripple-effect';
            ripple.style.left = targetX + 'px';
            ripple.style.top = targetY + 'px';
            document.documentElement.appendChild(ripple);

            setTimeout(() => {
                if (svg) svg.style.transform = 'scale(1)';
            }, 160);

            setTimeout(() => {
                if (ripple.parentNode) ripple.parentNode.removeChild(ripple);
            }, 650);
        }, 400);
    }

    window.__browseruse_fade_timer = setTimeout(() => {
        if (cursor) cursor.style.setProperty('opacity', '0', 'important');
    }, 1500);
}`

const jsAnnotateElements = `() => {
    const overlayId = 'browseruse-som-overlay';
    const old = document.getElementById(overlayId);
    if (old) old.remove();

    const container = document.createElement('div');
    container.id = overlayId;
    container.style.cssText = 'position:fixed;top:0;left:0;width:100vw;height:100vh;pointer-events:none;z-index:2147483640;';

    const elements = document.querySelectorAll('a, button, input, textarea, select, [role="button"], [role="link"], h1, h2, h3');
    let count = 0;
    elements.forEach((el) => {
        if (count >= 100) return;
        const rect = el.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) return;
        const style = window.getComputedStyle(el);
        if (style.display === 'none' || style.visibility === 'hidden') return;

        count++;
        const box = document.createElement('div');
        box.style.cssText = 'position:absolute;left:' + rect.left + 'px;top:' + rect.top + 'px;width:' + rect.width + 'px;height:' + rect.height + 'px;border:2px solid #ff3b30;box-sizing:border-box;border-radius:3px;background:rgba(255, 59, 48, 0.08);';

        const badge = document.createElement('div');
        badge.innerText = '[' + count + ']';
        badge.style.cssText = 'position:absolute;left:' + rect.left + 'px;top:' + Math.max(0, rect.top - 18) + 'px;background:#ff3b30;color:#ffffff;font-size:11px;font-weight:bold;font-family:monospace;padding:1px 4px;border-radius:3px;box-shadow:0 1px 3px rgba(0,0,0,0.4);line-height:14px;';

        container.appendChild(box);
        container.appendChild(badge);
    });
    document.documentElement.appendChild(container);
    return count;
}`

const jsRemoveAnnotateOverlay = `() => {
    const el = document.getElementById('browseruse-som-overlay');
    if (el) el.remove();
}`

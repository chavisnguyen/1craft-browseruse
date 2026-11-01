package main

import (
	"context"
	"fmt"
	"strings"
)

// PageSummary contains basic metadata and extracted interactive elements of a page.
type PageSummary struct {
	URL      string
	Title    string
	Elements string
}

// TabInfo represents high-level info about an open tab.
type TabInfo struct {
	ID      string `json:"tab_id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Profile string `json:"profile,omitempty"`
}

// Driver defines the primitives supported by a browser backend (rod or extension).
type Driver interface {
	Name() string
	Mode() string
	CheckAlive(ctx context.Context, handle string) bool
	NewTab(ctx context.Context, url string) (handle string, err error)
	ListTabs(ctx context.Context, handles []string) ([]TabInfo, error)
	CloseTab(ctx context.Context, handle string) error
	Navigate(ctx context.Context, handle, url string) error
	Snapshot(ctx context.Context, handle string) (PageSummary, error)
	Click(ctx context.Context, handle, selector string) error
	Hover(ctx context.Context, handle, selector string) error
	Type(ctx context.Context, handle, selector, text string) error
	PressKey(ctx context.Context, handle, key string) error
	Select(ctx context.Context, handle, selector, value string) error
	WaitFor(ctx context.Context, handle, selector string, timeoutMs int, state string) error
	UploadFile(ctx context.Context, handle, selector string, files []string) error
	DragAndDrop(ctx context.Context, handle, fromSelector, toSelector string) error
	Scroll(ctx context.Context, handle, direction string, amount int) error
	ActivateTab(ctx context.Context, handle string) error
	ConsoleLogs(ctx context.Context, handle string, clear bool) ([]string, error)
	Screenshot(ctx context.Context, handle string, annotate bool) ([]byte, error)
	Evaluate(ctx context.Context, handle, script string) (any, error)
	Close() error
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
	return fmt.Sprintf("**Tab ID**: `%s`\n**Current URL**: `%s`\n**Page Title**: %s\n\n**Interactive Elements**:\n```\n%s\n```",
		tabID, summary.URL, summary.Title, summary.Elements)
}

const jsExtractElements = `() => {
    const items = [];
    const elements = document.querySelectorAll('a, button, input, textarea, select, [role="button"], [role="link"], h1, h2, h3');
    elements.forEach((el, idx) => {
        if (idx > 100) return;
        const rect = el.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) return;
        const text = (el.innerText || el.value || el.placeholder || el.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ');
        const tag = el.tagName.toLowerCase();
        const role = el.getAttribute('role') || tag;
        if (text || tag === 'input' || tag === 'textarea') {
            let meta = [];
            if (el.id) meta.push('id="' + el.id + '"');
            if (el.name) meta.push('name="' + el.name + '"');
            if (el.type) meta.push('type="' + el.type + '"');
            const metaStr = meta.length > 0 ? ' (' + meta.join(' ') + ')' : '';
            items.push('- [' + role + '] "' + text + '"' + metaStr);
        }
    });
    return items.join('\n');
}`

const jsShowVirtualCursor = `(x, y, isClick) => {
    let styleEl = document.getElementById('browseruse-cursor-styles');
    if (!styleEl) {
        styleEl = document.createElement('style');
        styleEl.id = 'browseruse-cursor-styles';
        styleEl.textContent = ` + "`" + `
            @keyframes browseruse-click-ripple {
                0% { transform: scale(0.2); opacity: 1; border-width: 4px; }
                50% { opacity: 0.85; }
                100% { transform: scale(2.8); opacity: 0; border-width: 1px; }
            }
            .browseruse-ripple-effect {
                position: fixed !important;
                width: 44px !important;
                height: 44px !important;
                margin-left: -22px !important;
                margin-top: -22px !important;
                border-radius: 50% !important;
                border: 3.5px solid #ff2a2a !important;
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
            z-index: 2147483647 !important;
            pointer-events: none !important;
            transition: transform 0.24s cubic-bezier(0.22, 1, 0.36, 1) !important;
            will-change: transform !important;
            display: flex !important;
            align-items: center !important;
            gap: 6px !important;
            transform: translate3d(${x}px, ${y}px, 0) !important;
        ` + "`" + `;
        cursor.innerHTML = ` + "`" + `
            <svg width="32" height="32" viewBox="0 0 32 32" fill="none" style="filter: drop-shadow(0 0 8px rgba(255, 42, 42, 0.9)) drop-shadow(0 3px 6px rgba(0,0,0,0.6));">
                <path d="M5 3L14 26L18 17L27 13L5 3Z" fill="#ff2a2a" stroke="#ffffff" stroke-width="2.5" stroke-linejoin="round"/>
                <circle cx="6" cy="4" r="3" fill="#ffffff" />
            </svg>
            <span style="background: linear-gradient(135deg, #ff2a2a, #ff6b6b); color: #ffffff; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; font-size: 11px; font-weight: 700; padding: 2px 7px; border-radius: 6px; box-shadow: 0 2px 8px rgba(0,0,0,0.4); border: 1.5px solid #ffffff; letter-spacing: 0.3px; white-space: nowrap;">🤖 AI Agent</span>
        ` + "`" + `;
        document.documentElement.appendChild(cursor);
    } else {
        cursor.style.transform = ` + "`translate3d(${x}px, ${y}px, 0)`" + `;
    }

    if (isClick) {
        const ripple = document.createElement('div');
        ripple.className = 'browseruse-ripple-effect';
        ripple.style.left = ` + "`${x}px`" + `;
        ripple.style.top = ` + "`${y}px`" + `;
        document.documentElement.appendChild(ripple);
        setTimeout(() => { if (ripple.parentNode) ripple.parentNode.removeChild(ripple); }, 650);
    }
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

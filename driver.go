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
	Type(ctx context.Context, handle, selector, text string) error
	PressKey(ctx context.Context, handle, key string) error
	Scroll(ctx context.Context, handle, direction string, amount int) error
	Screenshot(ctx context.Context, handle string) ([]byte, error)
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

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

// TabSummary is one tab inside the MCP tab group.
type TabSummary struct {
	TabID int    `json:"tabId"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// TabsContext describes the MCP tab group (mirrors Claude in Chrome's tabs_context_mcp).
type TabsContext struct {
	Exists     bool
	TabGroupID int
	Tabs       []TabSummary
}

// TabContextInfo is the tab list the extension attaches to every page tool result.
type TabContextInfo struct {
	CurrentTabID    int          `json:"currentTabId"`
	ExecutedOnTabID int          `json:"executedOnTabId"`
	AvailableTabs   []TabSummary `json:"availableTabs"`
	TabCount        int          `json:"tabCount"`
	TabGroupID      int          `json:"tabGroupId"`
}

// ToolResponse is the generic result of a page.* extension call (text, optional image, tab context).
type ToolResponse struct {
	Output      string          `json:"output"`
	Base64Image string          `json:"base64Image"`
	ImageFormat string          `json:"imageFormat"`
	ImageID     string          `json:"imageId"`
	TabContext  *TabContextInfo `json:"tabContext"`
}

// Driver defines the primitives supported by a browser backend (extension bridge or headless fallback).
type Driver interface {
	Name() string
	Mode() string
	CheckAlive(ctx context.Context, handle string) bool
	NewTab(ctx context.Context) (handle string, err error)
	TabsContext(ctx context.Context, createIfEmpty bool) (TabsContext, error)
	CloseTab(ctx context.Context, handle string) error
	Invoke(ctx context.Context, handle, method string, params map[string]any) (*ToolResponse, error)
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
	return fmt.Sprintf("**Tab ID**: `%s`\n**Current URL**: `%s`\n**Page Title**: %s\n\n**Accessibility Tree Elements** (target with `@e1`, `@e2` or selector):\n```\n%s\n```",
		tabID, summary.URL, summary.Title, summary.Elements)
}

func formatPageSummaryCompact(tabID string, summary PageSummary) string {
	return fmt.Sprintf("**Tab ID**: `%s` | **Current URL**: `%s` | **Page Title**: %s",
		tabID, summary.URL, summary.Title)
}

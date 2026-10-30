package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestExtractElementsJS(t *testing.T) {
	if !strings.Contains(jsExtractElements, "querySelectorAll") {
		t.Fatalf("expected jsExtractElements to contain querySelectorAll")
	}
}

func TestCDPOpenCheck(t *testing.T) {
	isOpen := defaultRodDriver.isCDPOpen("http://127.0.0.1:9222")
	t.Logf("CDP port 9222 open: %v", isOpen)
}

func TestBrowserMultiTab(t *testing.T) {
	os.Setenv("BROWSER_HEADLESS", "true")
	defer os.Unsetenv("BROWSER_HEADLESS")

	ctx := context.Background()
	router := NewRouter(defaultRodDriver)

	// 1. Create first tab
	tabID1, summary1, err := router.NewTab(ctx, "https://example.com", "")
	if err != nil {
		t.Fatalf("Failed to open tab 1: %v", err)
	}
	defer router.CloseAll(ctx)

	if tabID1 != "tab-1" {
		t.Errorf("expected tabID 'tab-1', got '%s'", tabID1)
	}

	summaryStr1 := formatPageSummary(tabID1, summary1)
	if !strings.Contains(summaryStr1, "Example Domain") {
		t.Errorf("expected summary to contain 'Example Domain'")
	}

	// 2. Create second tab concurrently
	tabID2, _, err := router.NewTab(ctx, "https://example.org", "")
	if err != nil {
		t.Fatalf("Failed to open tab 2: %v", err)
	}

	if tabID2 != "tab-2" {
		t.Errorf("expected tabID 'tab-2', got '%s'", tabID2)
	}

	tabs, _ := router.ListTabs(ctx)
	if len(tabs) < 2 {
		t.Errorf("expected at least 2 open tabs, got %d", len(tabs))
	}

	// 3. Close tab 2
	if _, err := router.CloseTab(ctx, tabID2); err != nil {
		t.Errorf("failed to close tab 2: %v", err)
	}
}

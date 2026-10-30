package main

import (
	"context"
	"strings"
	"testing"
)

// TestRouter_TabAutoRecovery tests that when a tab is closed outside the router
// (e.g. by user closing the tab or browser restart):
// (a) Querying with the old tab_id returns the helpful user guidance error.
// (b) Querying without tab_id prunes the dead tab and automatically creates a new tab.
func TestRouter_TabAutoRecovery(t *testing.T) {
	ctx := context.Background()
	rod := NewRodDriver()
	router := NewRouter(rod)
	defer router.CloseAll(ctx)

	// 1. Open a tab
	tabID, _, err := router.NewTab(ctx, "data:text/html,<h1>Tab 1</h1>", "")
	if err != nil {
		t.Fatalf("failed to create tab: %v", err)
	}

	ref, err := router.GetTab(ctx, tabID)
	if err != nil {
		t.Fatalf("failed to get tab ref: %v", err)
	}

	// 2. Close the tab directly from the driver (simulating external close)
	if err := rod.CloseTab(ctx, ref.handle); err != nil {
		t.Fatalf("failed to close tab on driver: %v", err)
	}

	// (a) Calling Snapshot with the dead tab_id must return the expected guidance error
	expectedErr := "tab '" + tabID + "' not found or was closed. Call 'browser_list_tabs' or 'browser_new_tab'"
	_, _, err = router.Snapshot(ctx, tabID)
	if err == nil {
		t.Fatalf("expected error for closed tab, got nil")
	}
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}

	// (b) Calling Snapshot without tab_id must prune the dead tab and auto-create a new active tab
	newTabID, summary, err := router.Snapshot(ctx, "")
	if err != nil {
		t.Fatalf("expected auto-creation of new tab without tab_id, got error: %v", err)
	}
	if newTabID == tabID {
		t.Errorf("expected new tab ID to be different from dead tab ID %q, got %q", tabID, newTabID)
	}
	if !strings.HasPrefix(newTabID, "tab-") {
		t.Errorf("expected new tab ID prefix 'tab-', got %q", newTabID)
	}
	if summary.URL == "" && summary.Title == "" {
		t.Errorf("expected valid summary from auto-created tab")
	}

	// Verify router's internal activeTab is the new tab
	tabs, activeID := router.ListTabs(ctx)
	if activeID != newTabID {
		t.Errorf("expected active tab %q, got %q", newTabID, activeID)
	}
	if len(tabs) != 1 || tabs[0].ID != newTabID {
		t.Errorf("expected exactly 1 active tab %q, got %v", newTabID, tabs)
	}
}

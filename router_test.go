package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestRouter_TabAutoRecovery tests that when a tab is closed outside the router
// (e.g. by user closing the tab or browser restart):
// (a) Querying with the old tab_id returns the helpful user guidance error.
// (b) Querying without tab_id prunes the dead tab and automatically creates a new tab.
func TestRouter_TabAutoRecovery(t *testing.T) {
	ctx := context.Background()
	rod := NewRodDriver()
	router := NewRouter(rod, nil)
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

// TestRouter_ProfileSelection verifies the 4 profile selection rules from Section 2.4:
// - 0 profile online: fallback to rod
// - 1 profile online: auto-select that profile
// - >=2 profiles online: return error listing profiles
// - explicit profile: use if online, error if offline
func TestRouter_ProfileSelection(t *testing.T) {
	ctx := context.Background()
	rod := NewRodDriver()
	hub := NewExtHub(2 * time.Second)
	router := NewRouter(rod, hub)
	defer router.CloseAll(ctx)

	_, wsURL := startTestHubServer(t, hub)
	validToken, _ := GetOrCreateToken()
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
	}

	// Rule 1: 0 extension profiles online -> fallback to rod
	t.Run("ZeroProfiles_FallbackToRod", func(t *testing.T) {
		tabID, _, err := router.NewTab(ctx, "data:text/html,<h1>RodTab</h1>", "")
		if err != nil {
			t.Fatalf("expected fallback to rod, got err: %v", err)
		}
		ref, err := router.GetTab(ctx, tabID)
		if err != nil {
			t.Fatalf("failed to get tab ref: %v", err)
		}
		if ref.profile != "" || ref.driver.Name() != "rod" {
			t.Errorf("expected rod driver and empty profile, got %q, %s", ref.profile, ref.driver.Name())
		}
		_ = rod.CloseTab(ctx, ref.handle)
	})

	// Connect Client 1 ("work")
	connWork, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial work failed: %v", err)
	}
	defer connWork.Close(websocket.StatusNormalClosure, "")

	helloWork, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "work",
		InstanceID: "inst-work",
	})
	_ = connWork.Write(ctx, websocket.MessageText, helloWork)
	_, _, _ = connWork.Read(ctx) // read hello_ok

	// Helper to handle extension RPC calls on a mock client connection
	runMockExtensionResponder := func(conn *websocket.Conn) chan struct{} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				typ, data, err := conn.Read(context.Background())
				if err != nil || typ != websocket.MessageText {
					return
				}
				var req RequestMessage
				if err := json.Unmarshal(data, &req); err != nil {
					continue
				}

				var res ResultOrErr
				switch req.Method {
				case "tabs.create":
					resRaw, _ := json.Marshal(map[string]any{"tabId": 123, "url": "https://work.local", "title": "Work Domain"})
					res.result = resRaw
				case "page.snapshot":
					resRaw, _ := json.Marshal(map[string]any{"url": "https://work.local", "title": "Work Domain", "elements": "- [button] Work"})
					res.result = resRaw
				case "tabs.list":
					resRaw, _ := json.Marshal([]map[string]any{
						{"tabId": 123, "url": "https://work.local", "title": "Work Domain"},
					})
					res.result = resRaw
				}

				respMsg := ResponseMessage{
					Type:   "res",
					ID:     req.ID,
					Result: res.result,
				}
				respBytes, _ := json.Marshal(respMsg)
				_ = conn.Write(context.Background(), websocket.MessageText, respBytes)
			}
		}()
		return done
	}

	workResponderDone := runMockExtensionResponder(connWork)
	defer func() {
		_ = connWork.Close(websocket.StatusNormalClosure, "")
		<-workResponderDone
	}()

	// Rule 2: exactly 1 profile online, empty profile param -> auto-select "work"
	t.Run("SingleProfile_AutoSelect", func(t *testing.T) {
		tabID, _, err := router.NewTab(ctx, "https://work.local", "")
		if err != nil {
			t.Fatalf("expected auto-select of 'work', got error: %v", err)
		}
		ref, err := router.GetTab(ctx, tabID)
		if err != nil {
			t.Fatalf("failed to get tab ref: %v", err)
		}
		if ref.profile != "work" || ref.driver.Name() != "ext:work" {
			t.Errorf("expected ext:work driver, got %s, %s", ref.driver.Name(), ref.profile)
		}
	})

	// Connect Client 2 ("personal")
	connPersonal, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial personal failed: %v", err)
	}
	defer connPersonal.Close(websocket.StatusNormalClosure, "")

	helloPersonal, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "personal",
		InstanceID: "inst-personal",
	})
	_ = connPersonal.Write(ctx, websocket.MessageText, helloPersonal)
	_, _, _ = connPersonal.Read(ctx) // read hello_ok

	personalResponderDone := runMockExtensionResponder(connPersonal)
	defer func() {
		_ = connPersonal.Close(websocket.StatusNormalClosure, "")
		<-personalResponderDone
	}()

	// Rule 3: >= 2 profiles online, empty profile param -> return error listing profiles
	t.Run("MultipleProfiles_AmbiguousError", func(t *testing.T) {
		_, _, err := router.NewTab(ctx, "https://example.com", "")
		if err == nil {
			t.Fatalf("expected error when multiple profiles online without profile param, got nil")
		}
		if !strings.Contains(err.Error(), "multiple extension profiles online") {
			t.Errorf("expected error message mentioning multiple profiles, got %v", err)
		}
		if !strings.Contains(err.Error(), "work") || !strings.Contains(err.Error(), "personal") {
			t.Errorf("expected error to list both profiles, got %v", err)
		}
	})

	// Rule 4a: Explicit profile ("personal") when >=2 online -> uses "personal"
	t.Run("ExplicitProfile_Online", func(t *testing.T) {
		tabID, _, err := router.NewTab(ctx, "https://personal.local", "personal")
		if err != nil {
			t.Fatalf("expected success with explicit profile, got err: %v", err)
		}
		ref, err := router.GetTab(ctx, tabID)
		if err != nil {
			t.Fatalf("failed to get tab ref: %v", err)
		}
		if ref.profile != "personal" || ref.driver.Name() != "ext:personal" {
			t.Errorf("expected ext:personal driver, got %s, %s", ref.driver.Name(), ref.profile)
		}
	})

	// Rule 4b: Explicit profile that is offline -> returns error
	t.Run("ExplicitProfile_Offline", func(t *testing.T) {
		_, _, err := router.NewTab(ctx, "https://example.com", "nonexistent-profile")
		if err == nil {
			t.Fatalf("expected error for offline profile, got nil")
		}
		expected := "profile 'nonexistent-profile' is not online"
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error %q, got %q", expected, err.Error())
		}
	})
}

func TestRouter_Scroll(t *testing.T) {
	ctx := context.Background()
	rod := NewRodDriver()
	router := NewRouter(rod, nil)
	defer router.CloseAll(ctx)

	tabID, _, err := router.NewTab(ctx, "data:text/html,<div style='height:3000px;'>Long Page</div>", "")
	if err != nil {
		t.Fatalf("failed to create tab: %v", err)
	}

	// Test scroll down
	targetTab, err := router.Scroll(ctx, tabID, "down", 500)
	if err != nil {
		t.Fatalf("failed to scroll down: %v", err)
	}
	if targetTab != tabID {
		t.Errorf("expected tab %s, got %s", tabID, targetTab)
	}

	// Test scroll to top
	_, err = router.Scroll(ctx, tabID, "top", 0)
	if err != nil {
		t.Fatalf("failed to scroll top: %v", err)
	}

	// Test scroll to bottom
	_, err = router.Scroll(ctx, tabID, "bottom", 0)
	if err != nil {
		t.Fatalf("failed to scroll bottom: %v", err)
	}

	// Test scroll up
	_, err = router.Scroll(ctx, tabID, "up", 300)
	if err != nil {
		t.Fatalf("failed to scroll up: %v", err)
	}
}

type ResultOrErr struct {
	result json.RawMessage
	err    error
}

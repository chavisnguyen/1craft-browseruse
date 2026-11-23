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

// TestRouter_ProfileSelection verifies the 4 profile selection rules from Section 2.4:
// - 0 profile online: error
// - 1 profile online: auto-select that profile
// - >=2 profiles online: return error listing profiles
// - explicit profile: use if online, error if offline
func TestRouter_ProfileSelection(t *testing.T) {
	ctx := context.Background()
	hub := NewExtHub(2 * time.Second)
	router := NewRouter(hub)
	defer router.CloseAll(ctx)

	_, wsURL := startTestHubServer(t, hub)
	validToken, _ := GetOrCreateToken()
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
	}

	// Rule 1: 0 extension profiles online -> error (no fallback backend)
	t.Run("ZeroProfiles_Error", func(t *testing.T) {
		_, _, err := router.NewTab(ctx, "")
		if err == nil || !strings.Contains(err.Error(), "no browser extension is online") {
			t.Fatalf("expected 'no browser extension is online' error, got %v", err)
		}
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
					resRaw, _ := json.Marshal(map[string]any{"tabId": 123, "tabGroupId": 7})
					res.result = resRaw
				case "tabs.context":
					resRaw, _ := json.Marshal(map[string]any{
						"exists": true, "tabGroupId": 7,
						"availableTabs": []map[string]any{{"tabId": 123, "url": "https://work.local", "title": "Work Domain"}},
					})
					res.result = resRaw
				case "tabs.list":
					resRaw, _ := json.Marshal(map[string]any{
						"tabs": []map[string]any{{"tabId": 123, "url": "https://work.local", "title": "Work Domain", "inMcpGroup": true}},
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
		tabID, _, err := router.NewTab(ctx, "")
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
		_, _, err := router.NewTab(ctx, "")
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
		tabID, _, err := router.NewTab(ctx, "personal")
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
		_, _, err := router.NewTab(ctx, "nonexistent-profile")
		if err == nil {
			t.Fatalf("expected error for offline profile, got nil")
		}
		expected := "profile 'nonexistent-profile' is not online"
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error %q, got %q", expected, err.Error())
		}
	})
}

type ResultOrErr struct {
	result json.RawMessage
	err    error
}

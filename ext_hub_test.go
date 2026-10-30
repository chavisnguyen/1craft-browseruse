package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Helper to start an httptest server wrapping ExtHub
func startTestHubServer(t *testing.T, hub *ExtHub) (*httptest.Server, string) {
	t.Helper()
	_, err := GetOrCreateToken()
	if err != nil {
		t.Fatalf("failed to get test token: %v", err)
	}

	ts := httptest.NewServer(hub)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	return ts, wsURL
}

// 1. Handshake Rejections: bad token, bad origin, bad label, bad protocol
func TestExtHub_Handshake_Rejections(t *testing.T) {
	hub := NewExtHub(1 * time.Second)
	ts, wsURL := startTestHubServer(t, hub)
	validToken, _ := GetOrCreateToken()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1a. Invalid Origin (not starting with chrome-extension://)
	t.Run("InvalidOrigin", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
		req.Header.Set("Origin", "https://malicious-web.com")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for bad origin, got %d", resp.StatusCode)
		}
	})

	// 1b. Bad Token
	t.Run("BadToken", func(t *testing.T) {
		opts := &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
		}
		conn, _, err := websocket.Dial(ctx, wsURL, opts)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		hello, _ := json.Marshal(HelloMessage{
			Type:       "hello",
			Protocol:   1,
			Token:      "wrong-token-value",
			Profile:    "testprofile",
			InstanceID: "inst-1",
		})
		_ = conn.Write(ctx, websocket.MessageText, hello)

		_, data, err := conn.Read(ctx)
		if err == nil {
			var errMsg HelloErrorMessage
			_ = json.Unmarshal(data, &errMsg)
			if errMsg.Code != "bad_token" {
				t.Errorf("expected code 'bad_token', got %q", errMsg.Code)
			}
		}
	})

	// 1c. Bad Profile Label
	t.Run("BadLabel", func(t *testing.T) {
		opts := &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
		}
		conn, _, err := websocket.Dial(ctx, wsURL, opts)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		hello, _ := json.Marshal(HelloMessage{
			Type:       "hello",
			Protocol:   1,
			Token:      validToken,
			Profile:    "INVALID LABEL WITH SPACES!",
			InstanceID: "inst-1",
		})
		_ = conn.Write(ctx, websocket.MessageText, hello)

		_, data, err := conn.Read(ctx)
		if err == nil {
			var errMsg HelloErrorMessage
			_ = json.Unmarshal(data, &errMsg)
			if errMsg.Code != "bad_label" {
				t.Errorf("expected code 'bad_label', got %q", errMsg.Code)
			}
		}
	})

	// 1d. Bad Protocol Version
	t.Run("BadProtocol", func(t *testing.T) {
		opts := &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
		}
		conn, _, err := websocket.Dial(ctx, wsURL, opts)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		hello, _ := json.Marshal(HelloMessage{
			Type:       "hello",
			Protocol:   99, // Mismatched protocol
			Token:      validToken,
			Profile:    "testprofile",
			InstanceID: "inst-1",
		})
		_ = conn.Write(ctx, websocket.MessageText, hello)

		_, data, err := conn.Read(ctx)
		if err == nil {
			var errMsg HelloErrorMessage
			_ = json.Unmarshal(data, &errMsg)
			if errMsg.Code != "bad_protocol" {
				t.Errorf("expected code 'bad_protocol', got %q", errMsg.Code)
			}
		}
	})
}

// 2. Duplicate Label: Same instance replaces, different instance rejected with label_in_use
func TestExtHub_DuplicateLabel(t *testing.T) {
	hub := NewExtHub(2 * time.Second)
	_, wsURL := startTestHubServer(t, hub)
	validToken, _ := GetOrCreateToken()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
	}

	// Connect Client 1 (instance 1)
	conn1, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("conn1 dial failed: %v", err)
	}
	defer conn1.Close(websocket.StatusNormalClosure, "")

	hello1, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "work",
		InstanceID: "instance-alpha",
	})
	_ = conn1.Write(ctx, websocket.MessageText, hello1)

	_, data1, err := conn1.Read(ctx)
	if err != nil {
		t.Fatalf("conn1 failed to read hello_ok: %v", err)
	}
	var okMsg1 HelloOkMessage
	_ = json.Unmarshal(data1, &okMsg1)
	if okMsg1.Type != "hello_ok" || okMsg1.Profile != "work" {
		t.Fatalf("conn1 expected hello_ok, got %s", string(data1))
	}

	// Connect Client 2 with DIFFERENT instanceId -> must be rejected with label_in_use
	conn2, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("conn2 dial failed: %v", err)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "")

	hello2, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "work",
		InstanceID: "instance-beta", // Different instance
	})
	_ = conn2.Write(ctx, websocket.MessageText, hello2)

	_, data2, err := conn2.Read(ctx)
	if err == nil {
		var errMsg2 HelloErrorMessage
		_ = json.Unmarshal(data2, &errMsg2)
		if errMsg2.Code != "label_in_use" {
			t.Errorf("expected conn2 to get 'label_in_use', got %q", errMsg2.Code)
		}
	}

	// Connect Client 3 with SAME instanceId ("instance-alpha") -> replaces Client 1
	conn3, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("conn3 dial failed: %v", err)
	}
	defer conn3.Close(websocket.StatusNormalClosure, "")

	hello3, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "work",
		InstanceID: "instance-alpha", // Reconnect same instance
	})
	_ = conn3.Write(ctx, websocket.MessageText, hello3)

	_, data3, err := conn3.Read(ctx)
	if err != nil {
		t.Fatalf("conn3 failed to read hello_ok: %v", err)
	}
	var okMsg3 HelloOkMessage
	_ = json.Unmarshal(data3, &okMsg3)
	if okMsg3.Type != "hello_ok" {
		t.Fatalf("conn3 expected hello_ok, got %s", string(data3))
	}

	// Verify conn1 was disconnected
	_, _, err = conn1.Read(ctx)
	if err == nil {
		t.Errorf("expected conn1 to be closed after replacement, but it was still open")
	}
}

// 3. Request Disconnect: pending request aborts with 'disconnected' when client disconnects
func TestExtHub_RequestDisconnect(t *testing.T) {
	hub := NewExtHub(5 * time.Second)
	_, wsURL := startTestHubServer(t, hub)
	validToken, _ := GetOrCreateToken()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
	}
	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	hello, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "disconnect-test",
		InstanceID: "inst-disc",
	})
	_ = conn.Write(ctx, websocket.MessageText, hello)
	_, _, _ = conn.Read(ctx) // read hello_ok

	session, ok := hub.GetSession("disconnect-test")
	if !ok {
		t.Fatalf("session not found in hub")
	}

	errChan := make(chan error, 1)
	go func() {
		_, err := session.Call(context.Background(), "tabs.create", nil)
		errChan <- err
	}()

	// Wait for daemon to send the request frame, then close connection abruptly
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed reading request frame: %v", err)
	}
	_ = conn.Close(websocket.StatusAbnormalClosure, "abrupt close")

	select {
	case err := <-errChan:
		if err == nil {
			t.Fatalf("expected error on disconnect, got nil")
		}
		if !strings.Contains(err.Error(), "disconnected") {
			t.Errorf("expected error containing 'disconnected', got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Call hung and did not complete after disconnect")
	}
}

// 4. Request Timeout and Context Cancellation
func TestExtHub_RequestTimeoutAndCancellation(t *testing.T) {
	// Configure short timeout of 100ms for test
	hub := NewExtHub(100 * time.Millisecond)
	_, wsURL := startTestHubServer(t, hub)
	validToken, _ := GetOrCreateToken()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
	}
	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hello, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "timeout-test",
		InstanceID: "inst-timeout",
	})
	_ = conn.Write(ctx, websocket.MessageText, hello)
	_, _, _ = conn.Read(ctx)

	session, ok := hub.GetSession("timeout-test")
	if !ok {
		t.Fatalf("session not found")
	}

	// 4a. Timeout test (client ignores request)
	t.Run("Timeout", func(t *testing.T) {
		start := time.Now()
		_, err := session.Call(context.Background(), "dummy.method", nil)
		elapsed := time.Since(start)

		if err == nil || !strings.Contains(err.Error(), "timeout") {
			t.Errorf("expected 'timeout' error, got %v", err)
		}
		if elapsed > 1*time.Second {
			t.Errorf("timeout took too long: %v", elapsed)
		}
	})

	// 4b. Context Cancellation test
	t.Run("ContextCancelled", func(t *testing.T) {
		callCtx, callCancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			callCancel()
		}()

		_, err := session.Call(callCtx, "dummy.method", nil)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got %v", err)
		}
	})
}

// 5. Large 5MB Payload: Ensure WebSocket read limit (32MB) handles ~5MB base64 screenshot
func TestExtHub_LargeMessage5MB(t *testing.T) {
	hub := NewExtHub(5 * time.Second)
	_, wsURL := startTestHubServer(t, hub)
	validToken, _ := GetOrCreateToken()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
	}
	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hello, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "large-payload",
		InstanceID: "inst-large",
	})
	_ = conn.Write(ctx, websocket.MessageText, hello)
	_, _, _ = conn.Read(ctx)

	session, ok := hub.GetSession("large-payload")
	if !ok {
		t.Fatalf("session not found")
	}

	// Client responder goroutine
	go func() {
		_, reqData, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var req RequestMessage
		_ = json.Unmarshal(reqData, &req)

		// Generate ~5 MB of dummy base64 string
		largeData := strings.Repeat("A", 5*1024*1024)
		resultJSON, _ := json.Marshal(map[string]string{
			"pngBase64": largeData,
		})

		res := ResponseMessage{
			Type:   "res",
			ID:     req.ID,
			Result: resultJSON,
		}
		data, _ := json.Marshal(res)
		_ = conn.Write(ctx, websocket.MessageText, data)
	}()

	resRaw, err := session.Call(ctx, "page.screenshot", nil)
	if err != nil {
		t.Fatalf("session.Call failed for 5MB payload: %v", err)
	}
	if len(resRaw) < 5*1024*1024 {
		t.Errorf("expected received payload >= 5MB, got %d bytes", len(resRaw))
	}
}

// 6. Ping / Pong
func TestExtHub_PingPong(t *testing.T) {
	hub := NewExtHub(2 * time.Second)
	_, wsURL := startTestHubServer(t, hub)
	validToken, _ := GetOrCreateToken()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
	}
	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hello, _ := json.Marshal(HelloMessage{
		Type:       "hello",
		Protocol:   1,
		Token:      validToken,
		Profile:    "ping-test",
		InstanceID: "inst-ping",
	})
	_ = conn.Write(ctx, websocket.MessageText, hello)
	_, _, _ = conn.Read(ctx) // read hello_ok

	// Send ping
	ping, _ := json.Marshal(map[string]string{"type": "ping"})
	if err := conn.Write(ctx, websocket.MessageText, ping); err != nil {
		t.Fatalf("failed to write ping: %v", err)
	}

	_, pongData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read pong: %v", err)
	}
	var pong map[string]string
	_ = json.Unmarshal(pongData, &pong)
	if pong["type"] != "pong" {
		t.Errorf("expected pong response, got %s", string(pongData))
	}
}

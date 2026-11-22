package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Force hermetic tests: headless isolated browser, do not attach to user's daily Chrome
	_ = os.Setenv("CHROME_CDP_URL", "none")
	_ = os.Setenv("BROWSER_HEADLESS", "true")
	code := m.Run()
	defaultRouter.CloseAll(context.Background())
	os.Exit(code)
}

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
	router := NewRouter(defaultRodDriver, nil)

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

func postMCP(t *testing.T, handler http.Handler, path string, sessionID string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Host = "localhost:3025"
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeMCPResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	raw := strings.TrimSpace(rec.Body.String())
	if strings.HasPrefix(raw, "data:") || strings.Contains(raw, "\ndata:") {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data != "" {
					raw = data
					break
				}
			}
		}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("failed to decode MCP JSON response (%q): %v", rec.Body.String(), err)
	}
	return out
}

func TestMCPStreamableHTTP_CodexFlow(t *testing.T) {
	handler := setupHTTPHandler(setupMCPServer())

	// 1. Initialize handshake (Streamable HTTP on /mcp)
	initRec := postMCP(t, handler, "/mcp", "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "codex-mcp-client",
				"version": "0.169.0",
			},
		},
	})
	if initRec.Code != http.StatusOK {
		t.Fatalf("initialize expected 200, got %d: %s", initRec.Code, initRec.Body.String())
	}
	sessionID := initRec.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatalf("expected Mcp-Session-Id response header")
	}
	initJSON := decodeMCPResponse(t, initRec)
	result, ok := initJSON["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected initialize result object, got %#v", initJSON)
	}
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "1craft-browseruse" {
		t.Errorf("expected serverInfo.name '1craft-browseruse', got %v", serverInfo["name"])
	}

	// 2. notifications/initialized
	notifRec := postMCP(t, handler, "/mcp", sessionID, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	if notifRec.Code != http.StatusAccepted && notifRec.Code != http.StatusOK {
		t.Fatalf("notifications/initialized expected 200/202, got %d: %s", notifRec.Code, notifRec.Body.String())
	}

	// 3. tools/list — verify all 14 tools and OpenAI/Codex-compatible object schemas
	listRec := postMCP(t, handler, "/mcp", sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("tools/list expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	listJSON := decodeMCPResponse(t, listRec)
	listResult := listJSON["result"].(map[string]any)
	tools := listResult["tools"].([]any)
	if len(tools) != 14 {
		t.Fatalf("expected 14 tools, got %d", len(tools))
	}
	for _, item := range tools {
		tool := item.(map[string]any)
		schema := tool["inputSchema"].(map[string]any)
		if schema["type"] != "object" {
			t.Errorf("tool %v expected inputSchema.type == 'object', got %v", tool["name"], schema["type"])
		}
		if _, ok := schema["properties"]; !ok {
			t.Errorf("tool %v missing inputSchema.properties", tool["name"])
		}
	}

	// 4. tools/call (get_browser_status)
	callRec := postMCP(t, handler, "/mcp", sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "get_browser_status",
			"arguments": map[string]any{},
		},
	})
	if callRec.Code != http.StatusOK {
		t.Fatalf("tools/call expected 200, got %d: %s", callRec.Code, callRec.Body.String())
	}
	callJSON := decodeMCPResponse(t, callRec)
	if _, hasErr := callJSON["error"]; hasErr {
		t.Fatalf("tools/call returned JSON-RPC error: %#v", callJSON["error"])
	}

	// 5. notifications/cancelled fast-path
	cancelRec := postMCP(t, handler, "/mcp", sessionID, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/cancelled",
		"params":  map[string]any{"requestId": 99},
	})
	if cancelRec.Code != http.StatusOK {
		t.Errorf("notifications/cancelled expected 200, got %d", cancelRec.Code)
	}

	// 6. OAuth well-known discovery returns 404 (no auth required)
	wkReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	wkRec := httptest.NewRecorder()
	handler.ServeHTTP(wkRec, wkReq)
	if wkRec.Code != http.StatusNotFound {
		t.Errorf("/.well-known/ expected 404, got %d", wkRec.Code)
	}

	// 7. Non-allowed Origin is rejected
	badReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{}`)))
	badReq.Header.Set("Origin", "https://evil.example")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusForbidden {
		t.Errorf("disallowed Origin expected 403, got %d", badRec.Code)
	}
}

func TestCopyDirAndExtensionConfig(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(src, "manifest.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(src, "sub", "a.js"), []byte("x"), 0644)
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if !hasManifest(dst) {
		t.Fatal("manifest not copied")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "a.js")); string(b) != "x" {
		t.Fatal("nested file not copied")
	}
	cfg := extensionConfigJSON("tok", "3025")
	if !strings.Contains(cfg, `"token": "tok"`) || !strings.Contains(cfg, "ws://127.0.0.1:3025/ext") {
		t.Fatalf("bad config: %s", cfg)
	}
}

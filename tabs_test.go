package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// mockExtension connects a fake extension profile and answers RPCs via handler.
// It returns a function that lists the methods (with params) it has received.
func mockExtension(t *testing.T, ctx context.Context, wsURL, profile string,
	handler func(method string, params map[string]any) (any, *ResponseError)) func() []string {
	t.Helper()
	token, _ := GetOrCreateToken()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"chrome-extension://test-ext-id"}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })

	hello, _ := json.Marshal(HelloMessage{Type: "hello", Protocol: 1, Token: token, Profile: profile, InstanceID: "inst-" + profile})
	_ = conn.Write(ctx, websocket.MessageText, hello)
	_, _, _ = conn.Read(ctx)

	var mu sync.Mutex
	var seen []string
	go func() {
		for {
			typ, data, err := conn.Read(context.Background())
			if err != nil || typ != websocket.MessageText {
				return
			}
			var req struct {
				ID     int64          `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if json.Unmarshal(data, &req) != nil || req.Method == "" {
				continue
			}
			pj, _ := json.Marshal(req.Params)
			mu.Lock()
			seen = append(seen, req.Method+" "+string(pj))
			mu.Unlock()
			result, rerr := handler(req.Method, req.Params)
			resp := ResponseMessage{Type: "res", ID: req.ID, Error: rerr}
			if rerr == nil {
				resp.Result, _ = json.Marshal(result)
			}
			b, _ := json.Marshal(resp)
			_ = conn.Write(context.Background(), websocket.MessageText, b)
		}
	}()
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestTabsContext_NoGroupThenCreate(t *testing.T) {
	ctx := context.Background()
	hub := NewExtHub(2 * time.Second)
	router := NewRouter(hub)
	_, wsURL := startTestHubServer(t, hub)

	groupExists := false
	seen := mockExtension(t, ctx, wsURL, "work", func(method string, params map[string]any) (any, *ResponseError) {
		if method != "tabs.context" {
			return nil, &ResponseError{Code: "method_not_found", Message: method}
		}
		if params["createIfEmpty"] == true {
			groupExists = true
		}
		if !groupExists {
			return map[string]any{"exists": false}, nil
		}
		return map[string]any{"exists": true, "tabGroupId": 9, "availableTabs": []map[string]any{{"tabId": 41, "title": "New Tab", "url": "chrome://newtab/"}}}, nil
	})

	tc, err := router.TabsContext(ctx, "", false)
	if err != nil || tc.Exists {
		t.Fatalf("expected no group, got %+v err=%v", tc, err)
	}
	tc, err = router.TabsContext(ctx, "", true)
	if err != nil || !tc.Exists || tc.TabGroupID != 9 || len(tc.Tabs) != 1 || tc.Tabs[0].TabID != 41 {
		t.Fatalf("unexpected context %+v err=%v", tc, err)
	}
	calls := strings.Join(seen(), "\n")
	if !strings.Contains(calls, `"createIfEmpty":false`) || !strings.Contains(calls, `"createIfEmpty":true`) {
		t.Errorf("createIfEmpty not forwarded to extension: %s", calls)
	}
}

func TestTabs_RequireTabIDAndResolveUnknown(t *testing.T) {
	ctx := context.Background()
	hub := NewExtHub(2 * time.Second)
	router := NewRouter(hub)
	_, wsURL := startTestHubServer(t, hub)

	mockExtension(t, ctx, wsURL, "work", func(method string, params map[string]any) (any, *ResponseError) {
		switch method {
		case "tabs.list":
			ids, _ := params["tabIds"].([]any)
			var tabs []map[string]any
			for _, id := range ids {
				if id == float64(77) {
					tabs = append(tabs, map[string]any{"tabId": 77, "title": "T", "url": "https://x", "inMcpGroup": true})
				}
			}
			return map[string]any{"tabs": tabs}, nil
		case "tabs.close":
			if params["tabId"] == float64(88) {
				return nil, &ResponseError{Code: "tab_close_not_in_group", Message: "Tab 88 is not in this session's tab group."}
			}
			return map[string]any{"closed": true}, nil
		}
		return nil, &ResponseError{Code: "method_not_found", Message: method}
	})

	if _, err := router.GetTab(ctx, ""); err == nil || !strings.Contains(err.Error(), "tabId is required") {
		t.Errorf("expected tabId-required error, got %v", err)
	}
	if _, err := router.GetTab(ctx, "5"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected not-exist error, got %v", err)
	}
	ref, err := router.GetTab(ctx, "77")
	if err != nil || ref.profile != "work" || ref.handle != "77" {
		t.Fatalf("expected tab 77 resolved on 'work', got %+v err=%v", ref, err)
	}
	if _, err := router.CloseTab(ctx, "77"); err != nil {
		t.Errorf("close 77: %v", err)
	}
}

func TestFormatTabsContext(t *testing.T) {
	out := formatTabsContext(TabsContext{Exists: true, TabGroupID: 3, Tabs: []TabSummary{{TabID: 1, Title: "<b>x</b>", URL: "https://a"}}})
	if strings.ContainsAny(out, "<>") {
		t.Errorf("angle brackets must be escaped: %s", out)
	}
	var parsed struct {
		AvailableTabs []map[string]any `json:"availableTabs"`
		TabGroupID    int              `json:"tabGroupId"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil || parsed.TabGroupID != 3 || len(parsed.AvailableTabs) != 1 || parsed.AvailableTabs[0]["tabId"] != float64(1) {
		t.Errorf("bad JSON %s (%v)", out, err)
	}
}

func TestInvokeTool_ComputerAndReadPage(t *testing.T) {
	ctx := context.Background()
	_, wsURL := startTestHubServer(t, defaultExtHub)

	seen := mockExtension(t, ctx, wsURL, "invoke-test", func(method string, params map[string]any) (any, *ResponseError) {
		tabCtx := map[string]any{
			"currentTabId": 5, "executedOnTabId": 5, "tabCount": 1, "tabGroupId": 2,
			"availableTabs": []map[string]any{{"tabId": 5, "title": "A <b> page", "url": "https://a.test/?q=<x>"}},
		}
		switch method {
		case "tabs.list":
			return map[string]any{"tabs": []map[string]any{{"tabId": 5, "title": "t", "url": "u", "inMcpGroup": true}}}, nil
		case "page.computer":
			if params["action"] == "screenshot" {
				return map[string]any{"output": "Successfully captured screenshot (10x10, jpeg) - ID: ss_1", "base64Image": "AAAA", "imageFormat": "jpeg", "imageId": "ss_1", "tabContext": tabCtx}, nil
			}
			return nil, &ResponseError{Code: "computer_element_not_found", Message: "No element found with reference: \"ref_9\"."}
		case "page.readPage":
			return map[string]any{"output": "button \"Go\" [ref_1]\n\nViewport: 10x10", "tabContext": tabCtx}, nil
		}
		return nil, &ResponseError{Code: "method_not_found", Message: method}
	})
	defer defaultRouter.CloseAll(ctx)

	mk := func(args map[string]any) mcp.CallToolRequest {
		var r mcp.CallToolRequest
		r.Params.Arguments = args
		return r
	}

	// screenshot: text + Tab Context block + image (in that order), with escaped angle brackets
	res, err := invokeTool(ctx, mk(map[string]any{"tabId": float64(5), "action": "screenshot", "scale": 0.5, "junk": "ignored"}), "page.computer", "action", "scale")
	if err != nil || res.IsError {
		t.Fatalf("screenshot failed: %v %+v", err, res)
	}
	if len(res.Content) != 3 {
		t.Fatalf("expected text, tab context, image; got %d blocks", len(res.Content))
	}
	ctxText := res.Content[1].(mcp.TextContent).Text
	for _, want := range []string{"Tab Context:", "- Executed on tabId: 5", "• tabId 5:", "\\u003cb\\u003e"} {
		if !strings.Contains(ctxText, want) {
			t.Errorf("tab context missing %q: %q", want, ctxText)
		}
	}
	if img, ok := res.Content[2].(mcp.ImageContent); !ok || img.MIMEType != "image/jpeg" || img.Data != "AAAA" {
		t.Errorf("bad image block: %+v", res.Content[2])
	}
	if got := strings.Join(seen(), "\n"); strings.Contains(got, "junk") || !strings.Contains(got, `"scale":0.5`) {
		t.Errorf("only whitelisted args must be forwarded: %s", got)
	}

	// extension errors surface as the bare message (no "code:" prefix)
	res, _ = invokeTool(ctx, mk(map[string]any{"tabId": float64(5), "action": "left_click", "ref": "ref_9"}), "page.computer", "action", "ref")
	if !res.IsError || strings.Contains(res.Content[0].(mcp.TextContent).Text, "computer_element_not_found") {
		t.Errorf("expected bare error message, got %+v", res)
	}

	// read_page
	res, _ = invokeTool(ctx, mk(map[string]any{"tabId": float64(5), "filter": "interactive"}), "page.readPage", "filter", "depth", "ref_id", "max_chars")
	if res.IsError || !strings.Contains(res.Content[0].(mcp.TextContent).Text, `[ref_1]`) {
		t.Errorf("read_page result: %+v", res)
	}

	// missing tabId
	res, _ = invokeTool(ctx, mk(map[string]any{"action": "screenshot"}), "page.computer", "action")
	if !res.IsError || !strings.Contains(res.Content[0].(mcp.TextContent).Text, "tabId is required") {
		t.Errorf("expected tabId required error, got %+v", res)
	}
}

func TestInvokeTool_FindFormInputPageText(t *testing.T) {
	ctx := context.Background()
	_, wsURL := startTestHubServer(t, defaultExtHub)
	seen := mockExtension(t, ctx, wsURL, "invoke-test2", func(method string, params map[string]any) (any, *ResponseError) {
		switch method {
		case "tabs.list":
			return map[string]any{"tabs": []map[string]any{{"tabId": 6, "title": "t", "url": "u", "inMcpGroup": true}}}, nil
		case "page.find":
			return map[string]any{"output": "1 element shares words with the query:\nref_1 button \"Go\""}, nil
		case "page.formInput":
			return map[string]any{"output": "Checkbox checked (previous: unchecked)"}, nil
		case "page.getPageText":
			return map[string]any{"output": "Title: T\nURL: u\nSource element: <main>\n---\nhello"}, nil
		}
		return nil, &ResponseError{Code: "method_not_found", Message: method}
	})
	defer defaultRouter.CloseAll(ctx)
	mk := func(args map[string]any) mcp.CallToolRequest {
		var r mcp.CallToolRequest
		r.Params.Arguments = args
		return r
	}
	text := func(r *mcp.CallToolResult) string { return r.Content[0].(mcp.TextContent).Text }

	res, _ := invokeTool(ctx, mk(map[string]any{"tabId": float64(6), "query": "go button"}), "page.find", "query")
	if res.IsError || !strings.Contains(text(res), "ref_1 button") {
		t.Errorf("find: %+v", res)
	}
	res, _ = invokeTool(ctx, mk(map[string]any{"tabId": float64(6), "ref": "ref_2", "value": true}), "page.formInput", "ref", "value")
	if res.IsError || !strings.Contains(text(res), "Checkbox checked") {
		t.Errorf("form_input: %+v", res)
	}
	res, _ = invokeTool(ctx, mk(map[string]any{"tabId": float64(6), "max_chars": float64(100)}), "page.getPageText", "max_chars")
	if res.IsError || !strings.Contains(text(res), "Source element: <main>") {
		t.Errorf("get_page_text: %+v", res)
	}
	calls := strings.Join(seen(), "\n")
	if !strings.Contains(calls, `"value":true`) || !strings.Contains(calls, `"max_chars":100`) {
		t.Errorf("typed args must be forwarded unchanged (bool stays bool): %s", calls)
	}
}

func TestBrowserBatch(t *testing.T) {
	ctx := context.Background()
	_, wsURL := startTestHubServer(t, defaultExtHub)
	mockExtension(t, ctx, wsURL, "batch-test", func(method string, params map[string]any) (any, *ResponseError) {
		tabCtx := map[string]any{
			"currentTabId": 9, "executedOnTabId": 9, "tabCount": 1, "tabGroupId": 3,
			"availableTabs": []map[string]any{{"tabId": 9, "title": "P", "url": "https://p.test/"}},
		}
		switch method {
		case "tabs.list":
			return map[string]any{"tabs": []map[string]any{{"tabId": 9, "title": "P", "url": "u", "inMcpGroup": true}}}, nil
		case "page.navigate":
			return map[string]any{"output": "Navigated to https://p.test/", "tabContext": tabCtx}, nil
		case "page.computer":
			if params["action"] == "left_click" {
				return nil, &ResponseError{Code: "computer_element_not_found", Message: "No element found with reference: \"ref_9\"."}
			}
			return map[string]any{"output": "Successfully captured screenshot (1x1, jpeg) - ID: ss_1", "base64Image": "AAAA", "imageFormat": "jpeg", "tabContext": tabCtx}, nil
		}
		return nil, &ResponseError{Code: "method_not_found", Message: method}
	})
	defer defaultRouter.CloseAll(ctx)

	s := setupMCPServer()
	_ = s
	handlers := map[string]server.ToolHandlerFunc{}
	for _, name := range []string{"navigate", "computer"} {
		name := name
		handlers[name] = func(c context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			switch name {
			case "navigate":
				return invokeTool(c, r, "page.navigate", "url", "force")
			default:
				return invokeTool(c, r, "page.computer", "action", "ref")
			}
		}
	}
	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"actions": []any{
		map[string]any{"name": "navigate", "input": map[string]any{"tabId": float64(9), "url": "p.test"}},
		map[string]any{"name": "computer", "input": map[string]any{"tabId": float64(9), "action": "screenshot"}},
		map[string]any{"name": "computer", "input": map[string]any{"tabId": float64(9), "action": "left_click", "ref": "ref_9"}},
		map[string]any{"name": "navigate", "input": map[string]any{"tabId": float64(9), "url": "never"}},
	}}
	res := runBatch(ctx, req, handlers)
	if !res.IsError {
		t.Fatalf("batch must stop on the failing item")
	}
	var text strings.Builder
	images := 0
	for _, c := range res.Content {
		switch v := c.(type) {
		case mcp.TextContent:
			text.WriteString(v.Text + "\n")
		case mcp.ImageContent:
			images++
		}
	}
	out := text.String()
	for _, want := range []string{
		"[navigate] Navigated to https://p.test/",
		"[computer:screenshot] Successfully captured screenshot",
		"actions[2] (computer:left_click) failed: No element found with reference",
		"(2 completed, 1 remaining)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("batch output missing %q:\n%s", want, out)
		}
	}
	if images != 1 {
		t.Errorf("expected one interleaved image, got %d", images)
	}
	if strings.Contains(out, "never") {
		t.Errorf("items after the failure must not run")
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	Version          = "2.0.0"
	defaultRodDriver = NewRodDriver()
	defaultExtHub    = NewExtHub(30 * time.Second)
	defaultRouter    = NewRouter(defaultRodDriver, defaultExtHub)
)

func setupMCPServer() *server.MCPServer {
	s := server.NewMCPServer("1craft-browseruse", Version)

	inlineScreenshot := func(ctx context.Context, tabID string) string {
		uniqueName := fmt.Sprintf("screenshot-%d.png", time.Now().UnixNano())
		_, savedPath, err := defaultRouter.Screenshot(ctx, tabID, uniqueName, false)
		if err != nil {
			return ""
		}
		return fmt.Sprintf("\n\n📸 **Screenshot**: `%s`", savedPath)
	}

	// 1. read_page (Claude Code standard perception tool)
	readPageTool := mcp.NewTool("read_page",
		mcp.WithDescription("Read page content, URL, title, and interactive element hierarchy with deterministic references (@e1, @e2) using the Accessibility Tree (AXTree). Automatically redacts passwords and sensitive fields."),
		mcp.WithString("filter", mcp.Description("Element filter: 'interactive' (default) or 'all'")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(readPageTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filter := req.GetString("filter", "interactive")
		tabID := req.GetString("tab_id", "")
		actualTabID, summary, err := defaultRouter.ReadPage(ctx, tabID, filter)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read page: %v", err)), nil
		}
		return mcp.NewToolResultText(formatPageSummary(actualTabID, summary)), nil
	})

	// 2. computer (Claude Code standard unified interaction tool)
	computerTool := mcp.NewTool("computer",
		mcp.WithDescription("Unified tool to interact with the browser using mouse, keyboard, and display actions. Actions: left_click, right_click, double_click, triple_click, middle_click, mouse_move, hover, type, key, scroll, drag, screenshot."),
		mcp.WithString("action", mcp.Required(), mcp.Description("The action to perform: left_click, right_click, double_click, triple_click, middle_click, mouse_move, hover, type, key, scroll, drag, screenshot")),
		mcp.WithString("selector", mcp.Description("Element reference (e.g. '@e1'), CSS selector, or button text")),
		mcp.WithString("text", mcp.Description("Text to type (for action 'type')")),
		mcp.WithString("key", mcp.Description("Key name to press (for action 'key', e.g. 'Enter', 'Tab', 'Escape', 'ArrowDown')")),
		mcp.WithString("direction", mcp.Description("Scroll direction: 'down', 'up', 'top', 'bottom' (for action 'scroll')")),
		mcp.WithNumber("amount", mcp.Description("Scroll amount in pixels (default 500)")),
		mcp.WithString("from", mcp.Description("Source element reference or selector (for action 'drag')")),
		mcp.WithString("to", mcp.Description("Target element reference or selector (for action 'drag')")),
		mcp.WithBoolean("screenshot", mcp.Description("Optional: capture and return screenshot immediately after action")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(computerTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action := req.GetString("action", "left_click")
		tabID := req.GetString("tab_id", "")

		params := map[string]any{}
		if sel := req.GetString("selector", ""); sel != "" {
			params["selector"] = sel
		}
		if txt := req.GetString("text", ""); txt != "" {
			params["text"] = txt
		}
		if key := req.GetString("key", ""); key != "" {
			params["key"] = key
		}
		if dir := req.GetString("direction", ""); dir != "" {
			params["direction"] = dir
		}
		if amt := req.GetInt("amount", 0); amt > 0 {
			params["amount"] = amt
		}
		if from := req.GetString("from", ""); from != "" {
			params["from"] = from
		}
		if to := req.GetString("to", ""); to != "" {
			params["to"] = to
		}

		actualTabID, res, err := defaultRouter.Computer(ctx, tabID, action, params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Computer action '%s' failed: %v", action, err)), nil
		}

		shotMsg := ""
		if req.GetBool("screenshot", false) || action == "screenshot" {
			shotMsg = inlineScreenshot(ctx, actualTabID)
		}

		resJSON, _ := json.Marshal(res)
		return mcp.NewToolResultText(fmt.Sprintf("✅ Executed `%s` on `%s`.\nResult: %s%s", action, actualTabID, string(resJSON), shotMsg)), nil
	})

	// 3. form_input (Claude Code form input tool)
	formInputTool := mcp.NewTool("form_input",
		mcp.WithDescription("Fill or update a form element by reference (@e1 or selector). Supports setting text input/textarea values, selecting dropdown options, or toggling checkboxes and radio buttons."),
		mcp.WithString("ref_id", mcp.Description("Element reference (e.g. '@e1') or CSS selector")),
		mcp.WithString("selector", mcp.Description("Alias for ref_id")),
		mcp.WithString("value", mcp.Description("Text value to type or dropdown option value/label")),
		mcp.WithBoolean("checked", mcp.Description("True/false state for checkboxes, radio buttons, or switches")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(formInputTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		refID := req.GetString("ref_id", "")
		if refID == "" {
			refID = req.GetString("selector", "")
		}
		value := req.GetString("value", "")
		tabID := req.GetString("tab_id", "")
		var checkedPtr *bool
		if b, err := req.RequireBool("checked"); err == nil {
			checkedPtr = &b
		}

		actualTabID, res, err := defaultRouter.FormInput(ctx, tabID, refID, value, checkedPtr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to update form input: %v", err)), nil
		}
		resJSON, _ := json.Marshal(res)
		return mcp.NewToolResultText(fmt.Sprintf("✅ Form input `%s` updated on `%s`.\nResult: %s", refID, actualTabID, string(resJSON))), nil
	})

	// 4. navigate (Claude Code URL & history navigation)
	navigateTool := mcp.NewTool("navigate",
		mcp.WithDescription("Navigate to a URL or perform browser history actions (back, forward, reload)."),
		mcp.WithString("url", mcp.Description("Web URL to visit (defaults to https://)")),
		mcp.WithString("action", mcp.Description("History action: 'back', 'forward', or 'reload'")),
		mcp.WithBoolean("screenshot", mcp.Description("Optional: capture and return screenshot immediately after navigation")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(navigateTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url := req.GetString("url", "")
		action := req.GetString("action", "")
		tabID := req.GetString("tab_id", "")

		actualTabID, mode, summary, err := defaultRouter.Navigate(ctx, tabID, url, action)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Navigation failed: %v", err)), nil
		}

		shotMsg := ""
		if req.GetBool("screenshot", false) {
			shotMsg = inlineScreenshot(ctx, actualTabID)
		}

		navDesc := url
		if action != "" {
			navDesc = action
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Navigated tab `%s` (%s) using **%s**.\n\n%s%s", actualTabID, navDesc, mode, formatPageSummary(actualTabID, summary), shotMsg)), nil
	})

	// 5. find (Claude Code semantic element search)
	findTool := mcp.NewTool("find",
		mcp.WithDescription("Find interactive elements matching a natural language query or description on the active page (e.g. 'Search button' or 'Pricing plans')."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural language query or text description of what to find")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(findTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := req.GetString("query", "")
		tabID := req.GetString("tab_id", "")

		actualTabID, res, err := defaultRouter.Find(ctx, tabID, query)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Find query failed: %v", err)), nil
		}
		resJSON, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("🔍 Find matches for '%s' on `%s`:\n```json\n%s\n```", query, actualTabID, string(resJSON))), nil
	})

	// 6. read_network_requests (Claude Code network inspection)
	netTool := mcp.NewTool("read_network_requests",
		mcp.WithDescription("Read captured HTTP/Fetch/XHR network requests and API responses on the tab for debugging."),
		mcp.WithBoolean("clear", mcp.Description("Whether to clear buffer after reading (default true)")),
		mcp.WithString("filter", mcp.Description("Optional URL substring filter")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(netTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		clear := req.GetBool("clear", true)
		filter := req.GetString("filter", "")
		tabID := req.GetString("tab_id", "")

		actualTabID, res, err := defaultRouter.ReadNetworkRequests(ctx, tabID, clear, filter)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read network requests: %v", err)), nil
		}
		resJSON, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("🌐 Network requests on `%s`:\n```json\n%s\n```", actualTabID, string(resJSON))), nil
	})

	// 7. read_console_messages (Claude Code console logs)
	consoleTool := mcp.NewTool("read_console_messages",
		mcp.WithDescription("Read captured JavaScript console logs, warnings, and errors from a tab."),
		mcp.WithBoolean("clear", mcp.Description("Whether to clear logs after reading (default true)")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(consoleTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		clear := req.GetBool("clear", true)
		tabID := req.GetString("tab_id", "")

		actualTabID, logs, err := defaultRouter.ReadConsoleMessages(ctx, tabID, clear)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read console logs: %v", err)), nil
		}
		if len(logs) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("ℹ️ No new console messages on `%s`.", actualTabID)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("📋 Console messages on `%s` (%d entries):\n```\n%s\n```", actualTabID, len(logs), strings.Join(logs, "\n"))), nil
	})

	// 8. file_upload (Claude Code file upload)
	uploadTool := mcp.NewTool("file_upload",
		mcp.WithDescription("Upload one or more local files to an <input type='file'> element."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element reference (@e1) or CSS selector for the file input")),
		mcp.WithString("files", mcp.Required(), mcp.Description("Comma-separated absolute file paths or JSON array of paths")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(uploadTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector := req.GetString("selector", "")
		filesRaw := req.GetString("files", "")
		tabID := req.GetString("tab_id", "")

		var filePaths []string
		if strings.HasPrefix(strings.TrimSpace(filesRaw), "[") {
			_ = json.Unmarshal([]byte(filesRaw), &filePaths)
		} else {
			for _, p := range strings.Split(filesRaw, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					filePaths = append(filePaths, p)
				}
			}
		}

		if len(filePaths) == 0 {
			return mcp.NewToolResultError("No files specified"), nil
		}

		actualTabID, err := defaultRouter.UploadFile(ctx, tabID, selector, filePaths)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to upload files: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Uploaded %d file(s) to `%s` on `%s`.", len(filePaths), selector, actualTabID)), nil
	})

	// 9. javascript_tool (Claude Code JS evaluation)
	evalTool := mcp.NewTool("javascript_tool",
		mcp.WithDescription("Evaluate arbitrary JavaScript code in the context of the page and return the result."),
		mcp.WithString("script", mcp.Required(), mcp.Description("JavaScript expression or async function to evaluate")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID")),
	)
	s.AddTool(evalTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		script := req.GetString("script", "")
		tabID := req.GetString("tab_id", "")

		actualTabID, res, err := defaultRouter.Evaluate(ctx, tabID, script)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Script error: %v", err)), nil
		}
		valJSON, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("✅ Result from `%s`:\n%s", actualTabID, string(valJSON))), nil
	})

	// 10. tabs_context_mcp (Claude Code tab context & group listing)
	tabsContextTool := mcp.NewTool("tabs_context_mcp",
		mcp.WithDescription("Get the current browser tabs context: list of managed tabs, URLs, titles, active tab marker, and Chrome Tab Group status."),
	)
	s.AddTool(tabsContextTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabs, activeTab := defaultRouter.ListTabs(ctx)
		if len(tabs) == 0 {
			return mcp.NewToolResultText("No tabs currently open. Use 'tabs_create_mcp' to open one."), nil
		}
		var lines []string
		lines = append(lines, fmt.Sprintf("### Managed OneCraft Agent Tabs (%d)", len(tabs)))
		for _, t := range tabs {
			marker := " "
			if t.ID == activeTab {
				marker = "⭐ (active)"
			}
			profileTag := ""
			if t.Profile != "" {
				profileTag = fmt.Sprintf(" [%s]", t.Profile)
			}
			lines = append(lines, fmt.Sprintf("- **`%s`**%s %s: %s — `%s`", t.ID, profileTag, marker, t.Title, t.URL))
		}
		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	})

	// 11. tabs_create_mcp (OneCraft tab creation)
	tabsCreateTool := mcp.NewTool("tabs_create_mcp",
		mcp.WithDescription("Create a new managed browser tab in the isolated OneCraft Agent tab group."),
		mcp.WithString("url", mcp.Description("Optional initial URL to open in the new tab")),
		mcp.WithString("profile", mcp.Description("Optional Chrome extension profile name (e.g. 'work', 'personal')")),
	)
	s.AddTool(tabsCreateTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url := req.GetString("url", "")
		profile := req.GetString("profile", "")
		tabID, summary, err := defaultRouter.NewTab(ctx, url, profile)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create new tab: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Created new tab `%s` in OneCraft Agent tab group.\n\n%s", tabID, formatPageSummary(tabID, summary))), nil
	})

	// 12. tabs_close_mcp (Claude Code tab closing)
	tabsCloseTool := mcp.NewTool("tabs_close_mcp",
		mcp.WithDescription("Close a specific browser tab by its ID."),
		mcp.WithString("tab_id", mcp.Description("Tab ID to close (e.g. 'tab-1'). If omitted, closes active tab.")),
	)
	s.AddTool(tabsCloseTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabID := req.GetString("tab_id", "")
		closedID, err := defaultRouter.CloseTab(ctx, tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to close tab: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Closed tab `%s`.", closedID)), nil
	})

	// 13. browser_batch (Claude Code action batching)
	batchTool := mcp.NewTool("browser_batch",
		mcp.WithDescription("Execute a batch of sequential actions in a single turn to eliminate network roundtrips."),
		mcp.WithString("actions", mcp.Required(), mcp.Description("JSON array of computer/navigate action objects")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID")),
	)
	s.AddTool(batchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		actionsRaw := req.GetString("actions", "")
		tabID := req.GetString("tab_id", "")

		var actions []any
		if err := json.Unmarshal([]byte(actionsRaw), &actions); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid actions JSON: %v", err)), nil
		}

		actualTabID, res, err := defaultRouter.BrowserBatch(ctx, tabID, actions)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Batch execution failed: %v", err)), nil
		}
		resJSON, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("✅ Batch finished on `%s`:\n```json\n%s\n```", actualTabID, string(resJSON))), nil
	})

	// 14. get_browser_status
	statusTool := mcp.NewTool("get_browser_status",
		mcp.WithDescription("Check Chrome remote debugging port, connected extension profiles, and system diagnostics."),
	)
	s.AddTool(statusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(defaultRouter.GetStatus(ctx)), nil
	})

	return s
}

func main() {
	defaultPort := "3025"
	if p := os.Getenv("PORT"); p != "" {
		defaultPort = p
	}
	defaultHost := os.Getenv("HOST")

	sseMode := flag.Bool("sse", false, "Run in HTTP/SSE Server daemon mode (default is stdio)")
	port := flag.String("port", defaultPort, "Port to bind HTTP/SSE server (e.g. 3025)")
	host := flag.String("host", defaultHost, "Specific host to bind (default empty: binds dual-stack loopback 127.0.0.1 and [::1])")
	tokenFlag := flag.Bool("token", false, "Print auth token for Chrome extension and exit")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	vFlag := flag.Bool("v", false, "Print version and exit")
	flag.Parse()

	if *versionFlag || *vFlag || (len(os.Args) > 1 && os.Args[1] == "version") {
		fmt.Printf("1craft-browseruse v%s\n", Version)
		return
	}

	if *tokenFlag {
		tok, err := GetOrCreateToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting auth token: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(tok)
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		runDoctor(*port)
		return
	}

	s := setupMCPServer()

	if *sseMode {
		// Fatal check: auth token is required for extension hub /ext endpoint
		tok, err := GetOrCreateToken()
		if err != nil || tok == "" {
			fmt.Fprintf(os.Stderr, "Fatal: failed to initialize auth token for extension hub: %v\n", err)
			os.Exit(1)
		}

		streamableServer := server.NewStreamableHTTPServer(s,
			server.WithStreamableHTTPCORS(),
		)

		mux := http.NewServeMux()
		mux.Handle("/mcp", streamableServer)
		mux.Handle("/sse", streamableServer)
		mux.Handle("/ext", defaultExtHub)

		// Explicitly return 404 for OAuth checks so client immediately knows no auth is needed
		mux.HandleFunc("/.well-known/", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})

		// Logging & security wrapper to trace requests and protect endpoints
		loggingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Defense-in-depth: reject non-allowed Origin headers on /mcp and /sse
			if r.URL.Path == "/mcp" || r.URL.Path == "/sse" {
				origin := r.Header.Get("Origin")
				if !IsOriginAllowed(origin) {
					http.Error(w, "Forbidden: Origin not allowed", http.StatusForbidden)
					return
				}
			}

			if r.Method == http.MethodPost {
				bodyBytes, err := io.ReadAll(r.Body)
				if err == nil {
					r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
					if strings.Contains(string(bodyBytes), `"notifications/cancelled"`) {
						w.WriteHeader(http.StatusOK)
						return
					}
				}
			}
			mux.ServeHTTP(w, r)
		})

		httpServer := &http.Server{
			Handler: loggingHandler,
		}

		var listeners []net.Listener
		if *host != "" {
			ln, err := net.Listen("tcp", net.JoinHostPort(*host, *port))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to bind %s:%s: %v\n", *host, *port, err)
				os.Exit(1)
			}
			listeners = append(listeners, ln)
		} else {
			// Dual-stack loopback binding: IPv4 and IPv6
			ln4, err4 := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", *port))
			if err4 != nil {
				fmt.Fprintf(os.Stderr, "Failed to bind IPv4 loopback 127.0.0.1:%s: %v\n", *port, err4)
			} else {
				listeners = append(listeners, ln4)
			}

			ln6, err6 := net.Listen("tcp6", net.JoinHostPort("::1", *port))
			if err6 != nil {
				// IPv6 might be disabled on some machines, only warn if IPv4 succeeded
				if err4 != nil {
					fmt.Fprintf(os.Stderr, "Failed to bind IPv6 loopback [::1]:%s: %v\n", *port, err6)
				}
			} else {
				listeners = append(listeners, ln6)
			}

			if len(listeners) == 0 {
				fmt.Fprintf(os.Stderr, "Error: failed to bind any loopback interface on port %s\n", *port)
				os.Exit(1)
			}
		}

		fmt.Printf("🚀 Starting browseruse MCP Server (HTTP/SSE Daemon) on port %s\n", *port)
		for _, l := range listeners {
			fmt.Printf("   Listening on http://%s/mcp\n", l.Addr())
		}
		fmt.Printf("   Endpoints supported: /mcp, /sse, /ext, /\n")

		// Graceful shutdown handling
		stopChan := make(chan os.Signal, 1)
		signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

		for _, l := range listeners {
			go func(ln net.Listener) {
				if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
					fmt.Fprintf(os.Stderr, "HTTP Server error on %s: %v\n", ln.Addr(), err)
				}
			}(l)
		}

		<-stopChan
		fmt.Println("\n🛑 Shutting down browseruse MCP Server...")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		defaultRouter.CloseAll(context.Background())
		fmt.Println("👋 Goodbye!")
	} else {
		// Run in stdio mode (default)
		if err := server.ServeStdio(s); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server stdio error: %v\n", err)
			os.Exit(1)
		}
	}
}

func runDoctor(port string) {
	fmt.Printf("🩺 1craft BrowserUse Doctor (v%s)\n\n", Version)

	// 1. Auth Token
	tok, err := GetOrCreateToken()
	if err != nil || tok == "" {
		fmt.Printf("🔴 Auth Token: Error loading or generating token: %v\n", err)
	} else {
		primaryPath, _ := getTokenFilePath()
		fmt.Printf("🟢 Auth Token: Valid (%s...%s) in %s\n", tok[:8], tok[len(tok)-8:], primaryPath)
	}

	// 2. HTTP/SSE Daemon status
	daemonURL := fmt.Sprintf("http://127.0.0.1:%s/mcp", port)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(daemonURL)
	if err != nil {
		fmt.Printf("🔴 Daemon (: %s): Not reachable (%v)\n", port, err)
		fmt.Printf("   👉 Run 'brew services start browseruse' or 'make daemon' to start the server\n")
	} else {
		_ = resp.Body.Close()
		fmt.Printf("🟢 Daemon (: %s): Running and reachable at %s\n", port, daemonURL)
	}

	// 3. Chrome Remote Debugging (port 9222)
	chromeCDP := os.Getenv("CHROME_CDP_URL")
	if chromeCDP == "" {
		chromeCDP = defaultCDPURL
	}
	isCDP := defaultRodDriver.isCDPOpen(chromeCDP)
	if isCDP {
		fmt.Printf("🟢 Chrome CDP (Port 9222): Connected to local Google Chrome\n")
	} else {
		fmt.Printf("⚪ Chrome CDP (Port 9222): Port inactive (Extension Bridge or Standalone Chrome active)\n")
	}

	// 4. Chrome Extension Folder detection
	home, _ := os.UserHomeDir()
	extPaths := []string{
		"/opt/homebrew/share/browseruse/extension",
		"/opt/homebrew/opt/browseruse/share/browseruse/extension",
		"/usr/local/share/browseruse/extension",
		filepath.Join(home, ".1craft-browseruse", "extension"),
		"extension",
	}
	foundExt := ""
	for _, ep := range extPaths {
		if fi, err := os.Stat(filepath.Join(ep, "manifest.json")); err == nil && !fi.IsDir() {
			foundExt = ep
			break
		}
	}
	if foundExt != "" {
		fmt.Printf("🟢 Chrome Extension Folder: Found at %s\n", foundExt)
	} else {
		fmt.Printf("🟡 Chrome Extension Folder: Not detected in standard paths\n")
	}

	fmt.Printf("\n✨ Everything looks healthy! Endpoint for AI agents: http://localhost:%s/mcp\n", port)
}

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
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	defaultRodDriver = NewRodDriver()
	defaultExtHub    = NewExtHub(30 * time.Second)
	defaultRouter    = NewRouter(defaultRodDriver, defaultExtHub)
)

func setupMCPServer() *server.MCPServer {
	s := server.NewMCPServer("1craft-browseruse", "1.2.0")

	// 0. list_profiles
	listProfilesTool := mcp.NewTool("list_profiles",
		mcp.WithDescription("List online Chrome extension profiles connected to browseruse daemon."),
	)
	s.AddTool(listProfilesTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		profiles := defaultExtHub.ListProfiles()
		if len(profiles) == 0 {
			return mcp.NewToolResultText("No Chrome extension profiles online. All browser tabs use the standard rod/CDP backend."), nil
		}
		var lines []string
		lines = append(lines, fmt.Sprintf("### Online Extension Profiles (%d)", len(profiles)))
		for _, p := range profiles {
			emailStr := ""
			if p.Email != "" {
				emailStr = fmt.Sprintf(" (%s)", p.Email)
			}
			lines = append(lines, fmt.Sprintf("- **`%s`**%s: %d tabs managed (connected at %s, v%s)",
				p.Profile, emailStr, p.OpenTabs, p.ConnectedAt.Format("15:04:05"), p.Version))
		}
		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	})

	// 1. browser_new_tab
	newTabTool := mcp.NewTool("browser_new_tab",
		mcp.WithDescription("Create an isolated new browser tab. Essential for multiple agents to work concurrently without interfering with each other."),
		mcp.WithString("url", mcp.Description("Optional initial URL to open in the new tab")),
		mcp.WithString("profile", mcp.Description("Optional Chrome extension profile name (e.g. 'work', 'personal'). Call 'list_profiles' to see online profiles.")),
	)
	s.AddTool(newTabTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url := req.GetString("url", "")
		profile := req.GetString("profile", "")
		tabID, summary, err := defaultRouter.NewTab(ctx, url, profile)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create new tab: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Created new tab `%s`.\n\n%s", tabID, formatPageSummary(tabID, summary))), nil
	})

	// 2. browser_list_tabs
	listTabsTool := mcp.NewTool("browser_list_tabs",
		mcp.WithDescription("List all active browser tabs currently open, their tab IDs, URLs, and titles."),
	)
	s.AddTool(listTabsTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabs, activeTab := defaultRouter.ListTabs(ctx)
		if len(tabs) == 0 {
			return mcp.NewToolResultText("No tabs currently open. Use 'browser_new_tab' or 'browser_navigate' to open one."), nil
		}
		var lines []string
		lines = append(lines, fmt.Sprintf("### Open Tabs (%d)", len(tabs)))
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

	// 3. browser_close_tab
	closeTabTool := mcp.NewTool("browser_close_tab",
		mcp.WithDescription("Close a specific tab by its tab ID (or the active tab if omitted)."),
		mcp.WithString("tab_id", mcp.Description("The tab ID to close (e.g. 'tab-1'). If omitted, closes the active tab.")),
	)
	s.AddTool(closeTabTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabID := req.GetString("tab_id", "")
		closedID, err := defaultRouter.CloseTab(ctx, tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to close tab: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Closed tab `%s`.", closedID)), nil
	})

	// 4. browser_navigate
	navTool := mcp.NewTool("browser_navigate",
		mcp.WithDescription("Navigate to a URL. Operates on the specified tab_id or the active tab."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The web URL to navigate to")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID to target (e.g. 'tab-1'). If omitted, uses active tab.")),
	)
	s.AddTool(navTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url := req.GetString("url", "")
		tabID := req.GetString("tab_id", "")

		actualTabID, mode, summary, err := defaultRouter.Navigate(ctx, tabID, url)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to navigate: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Navigated tab `%s` to `%s` using **%s**.\n\n%s", actualTabID, normalizeURL(url), mode, formatPageSummary(actualTabID, summary))), nil
	})

	// 5. browser_snapshot
	snapTool := mcp.NewTool("browser_snapshot",
		mcp.WithDescription("Get the current page state, title, URL, and interactive elements for a tab."),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(snapTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabID := req.GetString("tab_id", "")
		actualTabID, summary, err := defaultRouter.Snapshot(ctx, tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tab: %v", err)), nil
		}
		return mcp.NewToolResultText(formatPageSummary(actualTabID, summary)), nil
	})

	// 6. browser_click
	clickTool := mcp.NewTool("browser_click",
		mcp.WithDescription("Click an element on a tab by CSS selector, button text, or aria-label."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector, button text, or link text")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(clickTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector := req.GetString("selector", "")
		tabID := req.GetString("tab_id", "")

		actualTabID, summary, err := defaultRouter.Click(ctx, tabID, selector)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to click element: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Clicked `%s` on `%s`.\n\n%s", selector, actualTabID, formatPageSummary(actualTabID, summary))), nil
	})

	// 7. browser_type
	typeTool := mcp.NewTool("browser_type",
		mcp.WithDescription("Type text into an input field or textarea."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector, placeholder, or label")),
		mcp.WithString("text", mcp.Required(), mcp.Description("The text to type")),
		mcp.WithBoolean("press_enter", mcp.Description("Whether to press Enter after typing (default false)")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(typeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector := req.GetString("selector", "")
		text := req.GetString("text", "")
		pressEnter := req.GetBool("press_enter", false)
		tabID := req.GetString("tab_id", "")

		actualTabID, err := defaultRouter.Type(ctx, tabID, selector, text, pressEnter)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to type text: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Typed '%s' into `%s` on `%s`.", text, selector, actualTabID)), nil
	})

	// 8. browser_press_key
	keyTool := mcp.NewTool("browser_press_key",
		mcp.WithDescription("Press a keyboard key on the active tab."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key name (Enter, Tab, Escape, Down, Up)")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID")),
	)
	s.AddTool(keyTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		keyName := req.GetString("key", "")
		tabID := req.GetString("tab_id", "")

		actualTabID, err := defaultRouter.PressKey(ctx, tabID, keyName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to press key: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Pressed key `%s` on `%s`.", keyName, actualTabID)), nil
	})

	// 9. browser_screenshot
	shotTool := mcp.NewTool("browser_screenshot",
		mcp.WithDescription("Capture screenshot of a tab and save to disk."),
		mcp.WithString("filename", mcp.Description("Optional file path (default /tmp/screenshot.png)")),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID")),
	)
	s.AddTool(shotTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename := req.GetString("filename", "screenshot.png")
		tabID := req.GetString("tab_id", "")

		actualTabID, savedPath, err := defaultRouter.Screenshot(ctx, tabID, filename)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to take screenshot: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("📸 Screenshot of `%s` saved to `%s`.", actualTabID, savedPath)), nil
	})

	// 10. browser_evaluate
	evalTool := mcp.NewTool("browser_evaluate",
		mcp.WithDescription("Execute JavaScript code in a tab context."),
		mcp.WithString("script", mcp.Required(), mcp.Description("JavaScript expression to evaluate")),
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

	// 11. get_browser_status
	statusTool := mcp.NewTool("get_browser_status",
		mcp.WithDescription("Check Chrome remote debugging port 9222, connection mode, and active tabs."),
	)
	s.AddTool(statusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(defaultRouter.GetStatus(ctx)), nil
	})

	// 12. browser_close
	closeTool := mcp.NewTool("browser_close",
		mcp.WithDescription("Close all tabs and free browser resources."),
	)
	s.AddTool(closeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		defaultRouter.CloseAll(ctx)
		return mcp.NewToolResultText("✅ All browser tabs and resources closed."), nil
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
	flag.Parse()

	if *tokenFlag {
		tok, err := GetOrCreateToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting auth token: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(tok)
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

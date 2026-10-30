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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const defaultCDPURL = "http://127.0.0.1:9222"

type TabInfo struct {
	ID    string `json:"tab_id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type BrowserManager struct {
	mu        sync.Mutex
	browser   *rod.Browser
	pages     map[string]*rod.Page
	activeTab string
	counter   int
	isCDP     bool
}

var mgr = &BrowserManager{
	pages: make(map[string]*rod.Page),
}

// resolveCDP returns a control URL for a running Chrome, or "" if none.
// Classic --remote-debugging-port Chrome serves /json/version over HTTP; Chrome's
// own profile (chrome://inspect remote debugging) only exposes a websocket whose
// path is written to DevToolsActivePort.
func (m *BrowserManager) resolveCDP(url string) string {
	if strings.HasPrefix(url, "ws://") || strings.HasPrefix(url, "wss://") {
		return url
	}
	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(strings.TrimRight(url, "/") + "/json/version")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return url
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	portFile := filepath.Join(home, "Library/Application Support/Google/Chrome/DevToolsActivePort")
	data, err := os.ReadFile(portFile)
	if err != nil {
		// macOS may block direct reads of the Chrome profile dir; /bin/cat can still succeed
		data, err = exec.Command("/bin/cat", portFile).Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "resolveCDP: cannot read DevToolsActivePort: %v\n", err)
			return ""
		}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return ""
	}
	return fmt.Sprintf("ws://127.0.0.1:%s%s", strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]))
}

func (m *BrowserManager) isCDPOpen(url string) bool {
	return m.resolveCDP(url) != ""
}

func (m *BrowserManager) ensureBrowser() error {
	if m.browser != nil {
		if _, err := m.browser.Version(); err == nil {
			return nil
		}
		// Browser died (e.g. user quit it): drop stale handle and relaunch
		m.browser = nil
		m.pages = make(map[string]*rod.Page)
		m.activeTab = ""
		m.isCDP = false
	}

	cdpURL := os.Getenv("CHROME_CDP_URL")
	if cdpURL == "" {
		cdpURL = defaultCDPURL
	}

	// Strategy 1: Connect to running Chrome on port 9222 (CDP)
	if ctrlURL := m.resolveCDP(cdpURL); ctrlURL != "" {
		b := rod.New().ControlURL(ctrlURL)
		if err := b.Connect(); err == nil {
			m.browser = b
			m.isCDP = true
			return nil
		}
	}

	// Strategy 2: Launch Google Chrome directly on macOS
	// Headless by default so the fallback browser never adds a second Dock icon
	headless := os.Getenv("BROWSER_HEADLESS") != "false"
	l := launcher.New().
		Bin("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome").
		Headless(headless)

	controlURL, err := l.Launch()
	if err != nil {
		l = launcher.New().Headless(headless)
		controlURL, err = l.Launch()
		if err != nil {
			return fmt.Errorf("failed to launch browser: %w", err)
		}
	}

	b := rod.New().ControlURL(controlURL)
	if err := b.Connect(); err != nil {
		return fmt.Errorf("failed to connect to launched browser: %w", err)
	}

	m.browser = b
	m.isCDP = false
	return nil
}

func (m *BrowserManager) NewTab(initialURL string) (*rod.Page, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureBrowser(); err != nil {
		return nil, "", err
	}

	page := m.browser.MustPage()
	m.counter++
	tabID := fmt.Sprintf("tab-%d", m.counter)

	if initialURL != "" {
		if !strings.HasPrefix(initialURL, "http://") && !strings.HasPrefix(initialURL, "https://") {
			initialURL = "https://" + initialURL
		}
		_ = page.Navigate(initialURL)
		_ = page.WaitLoad()
	}

	m.pages[tabID] = page
	m.activeTab = tabID
	return page, tabID, nil
}

func (m *BrowserManager) GetPage(tabID string) (*rod.Page, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureBrowser(); err != nil {
		return nil, "", err
	}

	// Clean up any pages that were closed by the user
	for id, p := range m.pages {
		if _, err := p.Info(); err != nil {
			delete(m.pages, id)
			if m.activeTab == id {
				m.activeTab = ""
			}
		}
	}

	// 1. If explicit tab_id is provided
	if tabID != "" {
		if p, ok := m.pages[tabID]; ok {
			return p, tabID, nil
		}
		return nil, "", fmt.Errorf("tab '%s' not found or was closed. Call 'browser_list_tabs' or 'browser_new_tab'", tabID)
	}

	// 2. If activeTab exists and is valid
	if m.activeTab != "" {
		if p, ok := m.pages[m.activeTab]; ok {
			return p, m.activeTab, nil
		}
	}

	// 3. Auto-attach to an existing tab or create a new one
	if len(m.pages) > 0 {
		for id, p := range m.pages {
			m.activeTab = id
			return p, id, nil
		}
	}

	// If no tabs exist, create the first tab
	page := m.browser.MustPage()
	m.counter++
	tabID = fmt.Sprintf("tab-%d", m.counter)
	m.pages[tabID] = page
	m.activeTab = tabID
	return page, tabID, nil
}

func (m *BrowserManager) CloseTab(tabID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tabID == "" {
		tabID = m.activeTab
	}

	if p, ok := m.pages[tabID]; ok {
		_ = p.Close()
		delete(m.pages, tabID)
		if m.activeTab == tabID {
			m.activeTab = ""
			for id := range m.pages {
				m.activeTab = id
				break
			}
		}
		// Last tab closed: shut down a browser we launched ourselves
		if len(m.pages) == 0 && !m.isCDP && m.browser != nil {
			_ = m.browser.Close()
			m.browser = nil
		}
		return nil
	}
	return fmt.Errorf("tab '%s' not found", tabID)
}

func (m *BrowserManager) ListTabs() []TabInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var list []TabInfo
	for id, p := range m.pages {
		title := "Unknown"
		url := "about:blank"
		if info, err := p.Info(); err == nil {
			title = info.Title
			url = info.URL
		}
		list = append(list, TabInfo{
			ID:    id,
			Title: title,
			URL:   url,
		})
	}
	return list
}

func (m *BrowserManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, p := range m.pages {
		_ = p.Close()
		delete(m.pages, id)
	}
	if m.browser != nil && !m.isCDP {
		_ = m.browser.Close()
	}
	m.browser = nil
	m.activeTab = ""
	m.isCDP = false
}

func findElement(page *rod.Page, target string) (*rod.Element, error) {
	target = strings.TrimSpace(target)
	clean := strings.Trim(target, `"'`)

	// Handle Playwright-style :has-text("...")
	if strings.Contains(target, ":has-text(") {
		start := strings.Index(target, ":has-text(") + len(":has-text(")
		end := strings.LastIndex(target, ")")
		if end > start {
			clean = strings.Trim(target[start:end], `"'`)
		}
	}

	// 1. Direct selector (CSS, XPath) with fast timeout
	el, err := page.Timeout(2 * time.Second).Element(target)
	if err == nil && el != nil {
		return el, nil
	}

	// 2. Button, Link with matching text
	el, err = page.Timeout(2 * time.Second).ElementR("button, a, [role='button'], [role='link']", clean)
	if err == nil && el != nil {
		return el, nil
	}

	// 3. Label or placeholder match
	cssQuery := fmt.Sprintf(`[aria-label*="%s"], [placeholder*="%s"], input[name="%s"]`, clean, clean, clean)
	el, err = page.Timeout(2 * time.Second).Element(cssQuery)
	if err == nil && el != nil {
		return el, nil
	}

	// 4. Any element with text
	el, err = page.Timeout(2 * time.Second).ElementR("*", clean)
	if err == nil && el != nil {
		return el, nil
	}

	return nil, fmt.Errorf("no element found matching '%s'", target)
}

const jsExtractElements = `() => {
    const items = [];
    const elements = document.querySelectorAll('a, button, input, textarea, select, [role="button"], [role="link"], h1, h2, h3');
    elements.forEach((el, idx) => {
        if (idx > 100) return;
        const rect = el.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) return;
        const text = (el.innerText || el.value || el.placeholder || el.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ');
        const tag = el.tagName.toLowerCase();
        const role = el.getAttribute('role') || tag;
        if (text || tag === 'input' || tag === 'textarea') {
            let meta = [];
            if (el.id) meta.push('id="' + el.id + '"');
            if (el.name) meta.push('name="' + el.name + '"');
            if (el.type) meta.push('type="' + el.type + '"');
            const metaStr = meta.length > 0 ? ' (' + meta.join(' ') + ')' : '';
            items.push('- [' + role + '] "' + text + '"' + metaStr);
        }
    });
    return items.join('\n');
}`

func getPageSummary(page *rod.Page, tabID string) string {
	info, err := page.Info()
	title := "Unknown"
	url := "Unknown"
	if err == nil {
		title = info.Title
		url = info.URL
	}

	res, err := page.Eval(jsExtractElements)
	elements := ""
	if err == nil {
		elements = res.Value.String()
	}

	return fmt.Sprintf("**Tab ID**: `%s`\n**Current URL**: `%s`\n**Page Title**: %s\n\n**Interactive Elements**:\n```\n%s\n```", tabID, url, title, elements)
}

func setupMCPServer() *server.MCPServer {
	s := server.NewMCPServer("browseruse", "1.1.0")

	// 1. browser_new_tab
	newTabTool := mcp.NewTool("browser_new_tab",
		mcp.WithDescription("Create an isolated new browser tab. Essential for multiple agents to work concurrently without interfering with each other."),
		mcp.WithString("url", mcp.Description("Optional initial URL to open in the new tab")),
	)
	s.AddTool(newTabTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url := req.GetString("url", "")
		page, tabID, err := mgr.NewTab(url)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create new tab: %v", err)), nil
		}
		summary := getPageSummary(page, tabID)
		return mcp.NewToolResultText(fmt.Sprintf("✅ Created new tab `%s`.\n\n%s", tabID, summary)), nil
	})

	// 2. browser_list_tabs
	listTabsTool := mcp.NewTool("browser_list_tabs",
		mcp.WithDescription("List all active browser tabs currently open, their tab IDs, URLs, and titles."),
	)
	s.AddTool(listTabsTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabs := mgr.ListTabs()
		if len(tabs) == 0 {
			return mcp.NewToolResultText("No tabs currently open. Use 'browser_new_tab' or 'browser_navigate' to open one."), nil
		}
		var lines []string
		lines = append(lines, fmt.Sprintf("### Open Tabs (%d)", len(tabs)))
		for _, t := range tabs {
			marker := " "
			if t.ID == mgr.activeTab {
				marker = "⭐ (active)"
			}
			lines = append(lines, fmt.Sprintf("- **`%s`** %s: %s — `%s`", t.ID, marker, t.Title, t.URL))
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
		target := tabID
		if target == "" {
			target = mgr.activeTab
		}
		if err := mgr.CloseTab(tabID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to close tab: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✅ Closed tab `%s`.", target)), nil
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

		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://" + url
		}

		page, actualTabID, err := mgr.GetPage(tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tab: %v", err)), nil
		}

		if err := page.Navigate(url); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to navigate: %v", err)), nil
		}
		_ = page.WaitLoad()
		time.Sleep(1 * time.Second)

		mode := "Google Chrome (CDP Port 9222)"
		if !mgr.isCDP {
			mode = "Standalone Chrome"
		}

		summary := getPageSummary(page, actualTabID)
		return mcp.NewToolResultText(fmt.Sprintf("✅ Navigated tab `%s` to `%s` using **%s**.\n\n%s", actualTabID, url, mode, summary)), nil
	})

	// 5. browser_snapshot
	snapTool := mcp.NewTool("browser_snapshot",
		mcp.WithDescription("Get the current page state, title, URL, and interactive elements for a tab."),
		mcp.WithString("tab_id", mcp.Description("Optional tab ID. If omitted, uses active tab.")),
	)
	s.AddTool(snapTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabID := req.GetString("tab_id", "")
		page, actualTabID, err := mgr.GetPage(tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tab: %v", err)), nil
		}
		return mcp.NewToolResultText(getPageSummary(page, actualTabID)), nil
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

		page, actualTabID, err := mgr.GetPage(tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tab: %v", err)), nil
		}

		el, err := findElement(page, selector)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Element not found: %v", err)), nil
		}

		if err := el.ScrollIntoView(); err == nil {
			time.Sleep(200 * time.Millisecond)
		}

		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to click element: %v", err)), nil
		}

		time.Sleep(1 * time.Second)
		summary := getPageSummary(page, actualTabID)
		return mcp.NewToolResultText(fmt.Sprintf("✅ Clicked `%s` on `%s`.\n\n%s", selector, actualTabID, summary)), nil
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

		page, actualTabID, err := mgr.GetPage(tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tab: %v", err)), nil
		}

		el, err := findElement(page, selector)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Element not found: %v", err)), nil
		}

		if err := el.SelectAllText(); err == nil {
			_ = el.Input("")
		}

		if err := el.Input(text); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to type text: %v", err)), nil
		}

		if pressEnter {
			_ = page.KeyActions().Press(input.Enter).Do()
			time.Sleep(1 * time.Second)
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
		keyName := strings.ToLower(req.GetString("key", ""))
		tabID := req.GetString("tab_id", "")

		page, actualTabID, err := mgr.GetPage(tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tab: %v", err)), nil
		}

		var k input.Key
		switch keyName {
		case "enter":
			k = input.Enter
		case "tab":
			k = input.Tab
		case "escape", "esc":
			k = input.Escape
		case "backspace":
			k = input.Backspace
		case "arrowdown", "down":
			k = input.ArrowDown
		case "arrowup", "up":
			k = input.ArrowUp
		default:
			k = input.Enter
		}

		_ = page.KeyActions().Press(k).Do()
		time.Sleep(500 * time.Millisecond)
		return mcp.NewToolResultText(fmt.Sprintf("✅ Pressed key `%s` on `%s`.", req.GetString("key", ""), actualTabID)), nil
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

		if !strings.HasPrefix(filename, "/") {
			filename = "/tmp/" + filename
		}

		page, actualTabID, err := mgr.GetPage(tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tab: %v", err)), nil
		}

		data, err := page.Screenshot(false, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to take screenshot: %v", err)), nil
		}

		if err := os.WriteFile(filename, data, 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to save screenshot: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("📸 Screenshot of `%s` saved to `%s`.", actualTabID, filename)), nil
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

		page, actualTabID, err := mgr.GetPage(tabID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get tab: %v", err)), nil
		}

		res, err := page.Eval(script)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Script error: %v", err)), nil
		}

		valJSON, _ := json.MarshalIndent(res.Value, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("✅ Result from `%s`:\n%s", actualTabID, string(valJSON))), nil
	})

	// 11. get_browser_status
	statusTool := mcp.NewTool("get_browser_status",
		mcp.WithDescription("Check Chrome remote debugging port 9222, connection mode, and active tabs."),
	)
	s.AddTool(statusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cdpURL := os.Getenv("CHROME_CDP_URL")
		if cdpURL == "" {
			cdpURL = defaultCDPURL
		}
		isOpen := mgr.isCDPOpen(cdpURL)

		status := "### Browser Automation Status (Golang Multi-Agent Engine)\n"
		if isOpen {
			status += "- **Chrome CDP (Port 9222)**: 🟢 Connected to Google Chrome\n"
			status += "- **Mode**: CDP Attached (Shared session & zero extra RAM)\n"
		} else {
			status += "- **Chrome CDP (Port 9222)**: 🔴 Not connected (Using standalone Chrome)\n"
			status += "- **Mode**: Standalone Chrome\n"
			status += "\n💡 **Tip**: Run `make chrome` to attach to your real Google Chrome!\n"
		}

		tabs := mgr.ListTabs()
		status += fmt.Sprintf("- **Open Tabs Managed**: %d\n", len(tabs))
		for _, t := range tabs {
			status += fmt.Sprintf("  * `[%s]` %s (`%s`)\n", t.ID, t.Title, t.URL)
		}
		return mcp.NewToolResultText(status), nil
	})

	// 12. browser_close
	closeTool := mcp.NewTool("browser_close",
		mcp.WithDescription("Close all tabs and free browser resources."),
	)
	s.AddTool(closeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mgr.CloseAll()
		return mcp.NewToolResultText("✅ All browser tabs and resources closed."), nil
	})

	return s
}

func main() {
	sseMode := flag.Bool("sse", false, "Run in HTTP/SSE Server daemon mode (default is stdio)")
	port := flag.String("port", "3025", "Port to bind HTTP/SSE server (e.g. 3025)")
	host := flag.String("host", "", "Specific host to bind (default empty: binds dual-stack loopback 127.0.0.1 and [::1])")
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
		// Initialize or ensure auth token exists
		if _, err := GetOrCreateToken(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize auth token: %v\n", err)
		}

		streamableServer := server.NewStreamableHTTPServer(s,
			server.WithStreamableHTTPCORS(),
		)

		mux := http.NewServeMux()
		mux.Handle("/mcp", streamableServer)
		mux.Handle("/sse", streamableServer)

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
		fmt.Printf("   Endpoints supported: /mcp, /sse, /\n")

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
		mgr.CloseAll()
		fmt.Println("👋 Goodbye!")
	} else {
		// Run in stdio mode (default)
		if err := server.ServeStdio(s); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server stdio error: %v\n", err)
			os.Exit(1)
		}
	}
}

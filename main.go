package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const defaultCDPURL = "http://127.0.0.1:9222"

type BrowserManager struct {
	mu      sync.Mutex
	browser *rod.Browser
	page    *rod.Page
	isCDP   bool
}

var mgr = &BrowserManager{}

func (m *BrowserManager) isCDPOpen(url string) bool {
	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(strings.TrimRight(url, "/") + "/json/version")
	if err == nil && resp.StatusCode == 200 {
		resp.Body.Close()
		return true
	}
	return false
}

func (m *BrowserManager) GetPage() (*rod.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If page is still active and valid, return it
	if m.page != nil {
		if _, err := m.page.Info(); err == nil {
			return m.page, nil
		}
	}

	cdpURL := os.Getenv("CHROME_CDP_URL")
	if cdpURL == "" {
		cdpURL = defaultCDPURL
	}

	// Strategy 1: Connect to running Chrome on port 9222
	if m.isCDPOpen(cdpURL) {
		b := rod.New().ControlURL(cdpURL)
		if err := b.Connect(); err == nil {
			m.browser = b
			m.isCDP = true

			pages, err := b.Pages()
			if err == nil && len(pages) > 0 {
				m.page = pages[0]
			} else {
				m.page = b.MustPage()
			}
			return m.page, nil
		}
	}

	// Strategy 2: Launch Google Chrome directly on macOS
	headless := os.Getenv("BROWSER_HEADLESS") == "true"
	l := launcher.New().
		Bin("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome").
		Headless(headless)

	controlURL, err := l.Launch()
	if err != nil {
		// Fallback to default rod-managed chromium download if system Chrome fails
		l = launcher.New().Headless(headless)
		controlURL, err = l.Launch()
		if err != nil {
			return nil, fmt.Errorf("failed to launch browser: %w", err)
		}
	}

	b := rod.New().ControlURL(controlURL)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to launched browser: %w", err)
	}

	m.browser = b
	m.page = b.MustPage()
	m.isCDP = false
	return m.page, nil
}

func (m *BrowserManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser != nil {
		// If connected via CDP, don't kill the user's daily Chrome, just close our tab
		if m.isCDP {
			if m.page != nil {
				_ = m.page.Close()
			}
		} else {
			_ = m.browser.Close()
		}
	}
	m.page = nil
	m.browser = nil
	m.isCDP = false
}

// findElement tries to find an element with flexible locator strategies
func findElement(page *rod.Page, target string) (*rod.Element, error) {
	target = strings.TrimSpace(target)
	clean := strings.Trim(target, `"'`)

	// 1. Direct selector (CSS, XPath)
	el, err := page.Element(target)
	if err == nil && el != nil {
		return el, nil
	}

	// 2. Button, Link, or interactive element with matching text / regex
	el, err = page.ElementR("button, a, [role='button'], [role='link']", clean)
	if err == nil && el != nil {
		return el, nil
	}

	// 3. Label or placeholder match
	cssQuery := fmt.Sprintf(`[aria-label*="%s"], [placeholder*="%s"], input[name="%s"]`, clean, clean, clean)
	el, err = page.Element(cssQuery)
	if err == nil && el != nil {
		return el, nil
	}

	// 4. Any element with text
	el, err = page.ElementR("*", clean)
	if err == nil && el != nil {
		return el, nil
	}

	return nil, fmt.Errorf("no element found matching '%s'", target)
}

const jsExtractElements = `() => {
    const items = [];
    const elements = document.querySelectorAll('a, button, input, textarea, select, [role="button"], [role="link"], h1, h2, h3');
    elements.forEach((el, idx) => {
        if (idx > 100) return; // limit to first 100 elements
        const rect = el.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) return; // skip hidden
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

func getPageSummary(page *rod.Page) string {
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

	return fmt.Sprintf("**Current URL**: `%s`\n**Page Title**: %s\n\n**Interactive Elements**:\n```\n%s\n```", url, title, elements)
}

func main() {
	s := server.NewMCPServer("browseruse", "1.0.0")

	// 1. browser_navigate
	navTool := mcp.NewTool("browser_navigate",
		mcp.WithDescription("Open or navigate to a web URL in the browser. Returns page title, URL, and list of interactive elements."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The web URL to navigate to (e.g. 'http://localhost:5173' or 'https://google.com')")),
	)
	s.AddTool(navTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url := req.GetString("url", "")
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://" + url
		}

		page, err := mgr.GetPage()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get browser: %v", err)), nil
		}

		if err := page.Navigate(url); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to navigate to %s: %v", url, err)), nil
		}
		_ = page.WaitLoad()
		time.Sleep(1 * time.Second)

		mode := "Google Chrome (CDP Port 9222)"
		if !mgr.isCDP {
			mode = "Standalone Chrome"
		}

		summary := getPageSummary(page)
		return mcp.NewToolResultText(fmt.Sprintf("✅ Navigated to `%s` using **%s**.\n\n%s", url, mode, summary)), nil
	})

	// 2. browser_snapshot
	snapTool := mcp.NewTool("browser_snapshot",
		mcp.WithDescription("Get the current page state, title, URL, and visible interactive elements (buttons, inputs, links)."),
	)
	s.AddTool(snapTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		page, err := mgr.GetPage()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get browser: %v", err)), nil
		}
		return mcp.NewToolResultText(getPageSummary(page)), nil
	})

	// 3. browser_click
	clickTool := mcp.NewTool("browser_click",
		mcp.WithDescription("Click an element on the active page by CSS selector, button text, or aria-label."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector, button text, or link text (e.g. 'Đăng nhập', 'button:has-text(\"Save\")', '#submit')")),
	)
	s.AddTool(clickTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector := req.GetString("selector", "")
		page, err := mgr.GetPage()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get browser: %v", err)), nil
		}

		el, err := findElement(page, selector)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Could not find element to click: %v", err)), nil
		}

		if err := el.ScrollIntoView(); err == nil {
			time.Sleep(200 * time.Millisecond)
		}

		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to click element: %v", err)), nil
		}

		time.Sleep(1 * time.Second)
		summary := getPageSummary(page)
		return mcp.NewToolResultText(fmt.Sprintf("✅ Clicked `%s`.\n\n**Updated Page State**:\n%s", selector, summary)), nil
	})

	// 4. browser_type
	typeTool := mcp.NewTool("browser_type",
		mcp.WithDescription("Type text into an input field or textarea."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector, placeholder, or label of the input")),
		mcp.WithString("text", mcp.Required(), mcp.Description("The text to type")),
		mcp.WithBoolean("press_enter", mcp.Description("Whether to press Enter after typing (default false)")),
	)
	s.AddTool(typeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector := req.GetString("selector", "")
		text := req.GetString("text", "")
		pressEnter := req.GetBool("press_enter", false)

		page, err := mgr.GetPage()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get browser: %v", err)), nil
		}

		el, err := findElement(page, selector)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Could not find input element: %v", err)), nil
		}

		if err := el.SelectAllText(); err == nil {
			_ = el.Input("") // clear existing
		}

		if err := el.Input(text); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to type text: %v", err)), nil
		}

		if pressEnter {
			_ = page.KeyActions().Press(input.Enter).Do()
			time.Sleep(1 * time.Second)
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Typed '%s' into `%s`.", text, selector)), nil
	})

	// 5. browser_press_key
	keyTool := mcp.NewTool("browser_press_key",
		mcp.WithDescription("Press a keyboard key on the active page (e.g. 'Enter', 'Tab', 'Escape', 'ArrowDown')."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key name to press")),
	)
	s.AddTool(keyTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		keyName := strings.ToLower(req.GetString("key", ""))
		page, err := mgr.GetPage()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get browser: %v", err)), nil
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
		return mcp.NewToolResultText(fmt.Sprintf("✅ Pressed key `%s`.", req.GetString("key", ""))), nil
	})

	// 6. browser_screenshot
	shotTool := mcp.NewTool("browser_screenshot",
		mcp.WithDescription("Capture a screenshot of the current page and save to disk."),
		mcp.WithString("filename", mcp.Description("Optional file name or path (default /tmp/screenshot.png)")),
	)
	s.AddTool(shotTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename := req.GetString("filename", "screenshot.png")
		if !strings.HasPrefix(filename, "/") {
			filename = "/tmp/" + filename
		}

		page, err := mgr.GetPage()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get browser: %v", err)), nil
		}

		data, err := page.Screenshot(false, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to take screenshot: %v", err)), nil
		}

		if err := os.WriteFile(filename, data, 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to save screenshot: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("📸 Screenshot captured and saved to `%s`.", filename)), nil
	})

	// 7. browser_evaluate
	evalTool := mcp.NewTool("browser_evaluate",
		mcp.WithDescription("Execute JavaScript code in the browser context."),
		mcp.WithString("script", mcp.Required(), mcp.Description("JavaScript expression to evaluate")),
	)
	s.AddTool(evalTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		script := req.GetString("script", "")
		page, err := mgr.GetPage()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get browser: %v", err)), nil
		}

		res, err := page.Eval(script)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Script error: %v", err)), nil
		}

		valJSON, _ := json.MarshalIndent(res.Value, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("✅ Result:\n%s", string(valJSON))), nil
	})

	// 8. get_browser_status
	statusTool := mcp.NewTool("get_browser_status",
		mcp.WithDescription("Check Chrome remote debugging port 9222 and active browser status."),
	)
	s.AddTool(statusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cdpURL := os.Getenv("CHROME_CDP_URL")
		if cdpURL == "" {
			cdpURL = defaultCDPURL
		}
		isOpen := mgr.isCDPOpen(cdpURL)

		status := "### Browser Automation Status (Golang Engine)\n"
		if isOpen {
			status += "- **Chrome CDP (Port 9222)**: 🟢 Connected to Google Chrome\n"
			status += "- **Mode**: CDP Attached (Shared session, cookies & zero extra RAM)\n"
		} else {
			status += "- **Chrome CDP (Port 9222)**: 🔴 Not connected (Will launch visible Chrome)\n"
			status += "- **Mode**: Standalone Chrome\n"
			status += "\n💡 **Tip**: Run `make chrome` to attach to your real Google Chrome!\n"
		}
		return mcp.NewToolResultText(status), nil
	})

	// 9. browser_close
	closeTool := mcp.NewTool("browser_close",
		mcp.WithDescription("Close current browser tab or session to free resources."),
	)
	s.AddTool(closeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mgr.Close()
		return mcp.NewToolResultText("✅ Browser session closed."), nil
	})

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

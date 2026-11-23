package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	Version       = "2.0.1"
	defaultExtHub = NewExtHub(30 * time.Second)
	defaultRouter = NewRouter(defaultExtHub)
)

func setupMCPServer() *server.MCPServer {
	s := server.NewMCPServer("klik", Version)

	handlers := map[string]server.ToolHandlerFunc{}
	add := func(tool mcp.Tool, h server.ToolHandlerFunc) {
		s.AddTool(tool, h)
		handlers[tool.Name] = h
	}

	// 1. read_page
	readPageTool := mcp.NewTool("read_page",
		mcp.WithDescription("Get an accessibility tree representation of elements on the page. By default returns all elements including non-visible ones. Output is limited to 50000 characters. If the output exceeds this limit it is truncated at a line boundary, with a note giving the full size — pass a larger max_chars, or use depth/ref_id to focus on part of the page. Optionally filter for only interactive elements. If you don't have a valid tab ID, use tabs_context first to get available tabs."),
		mcp.WithString("filter", mcp.Enum("interactive", "all"), mcp.Description("Filter elements: \"interactive\" for buttons/links/inputs only, \"all\" for all elements including non-visible ones (default: all elements)")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to read from. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
		mcp.WithNumber("depth", mcp.Description("Maximum depth of the tree to traverse (default: 15). Use a smaller depth if output is too large.")),
		mcp.WithString("ref_id", mcp.Description("Reference ID of a parent element to read. Will return the specified element and all its children. Use this to focus on a specific part of the page when output is too large.")),
		mcp.WithNumber("max_chars", mcp.Description("Maximum characters for output (default: 50000). Set to a higher value if your client can handle large outputs.")),
	)
	add(readPageTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.readPage", "filter", "depth", "ref_id", "max_chars")
	})

	// 2. computer
	computerTool := mcp.NewTool("computer",
		mcp.WithDescription("Use a mouse and keyboard to interact with a web browser, and take screenshots. If you don't have a valid tab ID, use tabs_context first to get available tabs.\n* Whenever you intend to click on an element like an icon, you should consult a screenshot to determine the coordinates of the element before moving the cursor.\n* If you tried clicking on a program or link but it failed to load, even after waiting, try adjusting your click location so that the tip of the cursor visually falls on the element that you want to click.\n* Make sure to click any buttons, links, icons, etc with the cursor tip in the center of the element. Don't click boxes on their edges unless asked."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("left_click", "right_click", "type", "screenshot", "wait", "scroll", "key", "left_click_drag", "double_click", "triple_click", "zoom", "scroll_to", "hover"),
			mcp.Description("The action to perform:\n* `left_click`: Click the left mouse button at the specified coordinates.\n* `right_click`: Click the right mouse button at the specified coordinates to open context menus.\n* `double_click`: Double-click the left mouse button at the specified coordinates.\n* `triple_click`: Triple-click the left mouse button at the specified coordinates.\n* `type`: Type a string of text.\n* `screenshot`: Take a screenshot of the screen.\n* `wait`: Wait for a specified number of seconds.\n* `scroll`: Scroll up, down, left, or right at the specified coordinates.\n* `key`: Press a specific keyboard key.\n* `left_click_drag`: Drag from start_coordinate to coordinate.\n* `zoom`: Take a screenshot of a specific region for closer inspection.\n* `scroll_to`: Scroll an element into view using its element reference ID from read_page or find tools.\n* `hover`: Move the mouse cursor to the specified coordinates or element without clicking. Useful for revealing tooltips, dropdown menus, or triggering hover states.")),
		mcp.WithArray("coordinate", mcp.Items(map[string]any{"type": "number"}), mcp.MinItems(2), mcp.MaxItems(2),
			mcp.Description("(x, y): The x (pixels from the left edge) and y (pixels from the top edge) coordinates. Required for `left_click`, `right_click`, `double_click`, `triple_click`, and `scroll`. For `left_click_drag`, this is the end position.")),
		mcp.WithString("text", mcp.Description("The text to type (for `type` action) or the key(s) to press (for `key` action). For `key` action: Provide space-separated keys (e.g., \"Backspace Backspace Delete\"). Supports keyboard shortcuts using the platform's modifier key (use \"cmd\" on Mac, \"ctrl\" on Windows/Linux, e.g., \"cmd+a\" or \"ctrl+a\" for select all). Page zoom shortcuts (e.g. \"cmd+=\", \"ctrl+-\", \"cmd+0\") are not supported and will return an error - use the `zoom` action to magnify a region of the page instead.")),
		mcp.WithNumber("duration", mcp.Min(0), mcp.Max(10), mcp.Description("The number of seconds to wait. Required for `wait`. Maximum 10 seconds.")),
		mcp.WithString("scroll_direction", mcp.Enum("up", "down", "left", "right"), mcp.Description("The direction to scroll. Required for `scroll`.")),
		mcp.WithNumber("scroll_amount", mcp.Min(1), mcp.Max(10), mcp.Description("The number of scroll wheel ticks. Optional for `scroll`, defaults to 3.")),
		mcp.WithArray("start_coordinate", mcp.Items(map[string]any{"type": "number"}), mcp.MinItems(2), mcp.MaxItems(2),
			mcp.Description("(x, y): The starting coordinates for `left_click_drag`.")),
		mcp.WithArray("region", mcp.Items(map[string]any{"type": "number"}), mcp.MinItems(4), mcp.MaxItems(4),
			mcp.Description("(x0, y0, x1, y1): The rectangular region to capture for `zoom`. Coordinates define a rectangle from top-left (x0, y0) to bottom-right (x1, y1) in pixels from the viewport origin. Required for `zoom` action. Useful for inspecting small UI elements like icons, buttons, or text.")),
		mcp.WithNumber("scale", mcp.Min(0.1), mcp.Max(1), mcp.Description("For `screenshot` and `zoom` only. Scale factor in [0.1, 1] for the returned image; 1 (default) uses the full image token budget, 0.5 returns an image at half the width and height (~quarter of the tokens). Coordinates are ALWAYS in the full-resolution coordinate frame (reported with every scaled screenshot), never in the scaled image's own pixels.")),
		mcp.WithNumber("repeat", mcp.Min(1), mcp.Max(100), mcp.Description("Number of times to repeat the key sequence. Only applicable for `key` action. Must be a positive integer between 1 and 100. Default is 1. Useful for navigation tasks like pressing arrow keys multiple times.")),
		mcp.WithString("ref", mcp.Description("Element reference ID from read_page or find tools (e.g., \"ref_1\", \"ref_2\"). Required for `scroll_to` action. Can be used as alternative to `coordinate` for click actions.")),
		mcp.WithString("modifiers", mcp.Description("Modifier keys for click actions. Supports: \"ctrl\", \"shift\", \"alt\", \"cmd\" (or \"meta\"), \"win\" (or \"windows\"). Can be combined with \"+\" (e.g., \"ctrl+shift\", \"cmd+alt\"). Optional.")),
		mcp.WithString("action_summary", mcp.Description("A few words saying what this action does on the page and to what, for example 'Sends the drafted reply to pat@example.com' or 'Opens the Filters menu'. Set it on every left_click, right_click, double_click, triple_click, left_click_drag, key and type action. State the effect only, and accurately: no reasons, nothing about what you were asked or allowed to do, no passwords or other secrets.")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to execute the action on. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
	)
	add(computerTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.computer", "action", "coordinate", "text", "duration", "scroll_direction", "scroll_amount", "start_coordinate", "region", "scale", "repeat", "ref", "modifiers")
	})

	// 3. form_input
	formInputTool := mcp.NewTool("form_input",
		mcp.WithDescription("Set values in form elements using element reference ID from the read_page tool. If you don't have a valid tab ID, use tabs_context first to get available tabs."),
		mcp.WithString("ref", mcp.Required(), mcp.Description("Element reference ID from the read_page tool (e.g., \"ref_1\", \"ref_2\")")),
		mcp.WithAny("value", mcp.Required(), mcp.Description("The value to set. For checkboxes use boolean, for selects use option value or text, for other inputs use appropriate string/number")),
		mcp.WithString("action_summary", mcp.Description("A few words saying what this form fill does on the page and to what, for example 'Sets the delivery date to 29 September'. State the effect only, and accurately: no reasons, nothing about what you were asked or allowed to do, no passwords or other secrets.")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to set form value in. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
	)
	add(formInputTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.formInput", "ref", "value")
	})

	// 3b. get_page_text
	pageTextTool := mcp.NewTool("get_page_text",
		mcp.WithDescription("Extract raw text content from the page, prioritizing article content. Ideal for reading articles, blog posts, or other text-heavy pages. Returns plain text without HTML formatting. If you don't have a valid tab ID, use tabs_context first to get available tabs. Output is limited to 50000 characters by default. If the output exceeds this limit it is truncated at a line boundary, with a note giving the full size — pass a larger max_chars, or use read_page with a ref_id to focus on a smaller section."),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to extract text from. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
		mcp.WithNumber("max_chars", mcp.Description("Maximum characters for output (default: 50000). Set to a higher value if your client can handle large outputs.")),
	)
	add(pageTextTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.getPageText", "max_chars")
	})

	// 4. navigate
	navigateTool := mcp.NewTool("navigate",
		mcp.WithDescription("Navigate to a URL, or go forward/back in browser history. If you don't have a valid tab ID, use tabs_context first to get available tabs."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The URL to navigate to. Can be provided with or without protocol (defaults to https://). Use \"forward\" to go forward in history or \"back\" to go back in history.")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to navigate. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
		mcp.WithBoolean("force", mcp.Description("If the page shows a \"Leave site?\" dialog because of unsaved changes, discard those changes and navigate anyway. Defaults to false: navigation is blocked and an error is returned so you can decide first.")),
	)
	add(navigateTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.navigate", "url", "force")
	})

	// 5. find
	findTool := mcp.NewTool("find",
		mcp.WithDescription("Find elements on the page using natural language. Can search for elements by their purpose (e.g., \"search bar\", \"login button\") or by text content (e.g., \"organic mango product\"). Returns up to 20 matching elements with references that can be used with other tools. If more than 20 matches exist, you'll be notified to use a more specific query. If you don't have a valid tab ID, use tabs_context first to get available tabs."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural language description of what to find (e.g., \"search bar\", \"add to cart button\", \"product title containing organic\")")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to search in. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
	)
	add(findTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.find", "query")
	})

	// 6. read_network_requests
	netTool := mcp.NewTool("read_network_requests",
		mcp.WithDescription("Read HTTP network requests (XHR, Fetch, documents, images, etc.) from a specific tab. Useful for debugging API calls, monitoring network activity, or understanding what requests a page is making. Returns all network requests made by the current page, including cross-origin requests. Requests are automatically cleared when the page navigates to a different domain. If you don't have a valid tab ID, use tabs_context first to get available tabs."),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to read network requests from. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
		mcp.WithString("urlPattern", mcp.Description("Optional URL pattern to filter requests. Only requests whose URL as listed (auth values shown as REDACTED) contains this string will be returned (e.g., '/api/' to filter API calls, 'example.com' to filter by domain).")),
		mcp.WithBoolean("clear", mcp.Description("If true, clear the network requests after reading to avoid duplicates on subsequent calls. Default is false.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of requests to return. Defaults to 100. Increase only if you need more results.")),
	)
	add(netTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.networkRequests", "urlPattern", "clear", "limit")
	})

	// 7. read_console_messages
	consoleTool := mcp.NewTool("read_console_messages",
		mcp.WithDescription("Read browser console messages (console.log, console.error, console.warn, etc.) from a specific tab. Useful for debugging JavaScript errors, viewing application logs, or understanding what's happening in the browser console. Returns console messages from the current domain only. If you don't have a valid tab ID, use tabs_context first to get available tabs. IMPORTANT: Always provide a pattern to filter messages - without a pattern, you may get too many irrelevant messages."),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to read console messages from. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
		mcp.WithBoolean("onlyErrors", mcp.Description("If true, only return error and exception messages. Default is false (return all message types).")),
		mcp.WithBoolean("clear", mcp.Description("If true, clear the console messages after reading to avoid duplicates on subsequent calls. Default is false.")),
		mcp.WithString("pattern", mcp.Description("Regex pattern to filter console messages. Only messages matching this pattern will be returned (e.g., 'error|warning' to find errors and warnings, 'MyApp' to filter app-specific logs). You should always provide a pattern to avoid getting too many irrelevant messages.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of messages to return. Defaults to 100. Increase only if you need more results.")),
	)
	add(consoleTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.consoleMessages", "onlyErrors", "clear", "pattern", "limit")
	})

	// 8. file_upload
	uploadTool := mcp.NewTool("file_upload",
		mcp.WithDescription("Upload one or multiple files to a file input element on the page. Do not click on file upload buttons or file inputs — clicking opens a native file picker dialog that you cannot see or interact with. Instead, use read_page or find to locate the file input element, then use this tool with its ref to upload files directly. The files are passed as base64 data and are set on the input with change/input events."),
		mcp.WithString("ref", mcp.Required(), mcp.Description("Element reference ID of the file input from read_page or find tools (e.g., \"ref_1\", \"ref_2\").")),
		mcp.WithArray("files", mcp.Required(), mcp.Items(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"data":     map[string]any{"type": "string", "description": "Base64-encoded file contents"},
				"name":     map[string]any{"type": "string", "description": "File name (no directories)"},
				"mimeType": map[string]any{"type": "string", "description": "Optional MIME type, e.g. image/png"},
			},
			"required": []string{"data", "name"},
		}), mcp.Description("The files to upload (total size at most 10 MB).")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID where the file input is located. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
	)
	add(uploadTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.fileUpload", "ref", "files")
	})

	// 8b. upload_image
	uploadImageTool := mcp.NewTool("upload_image",
		mcp.WithDescription("Upload a previously captured screenshot or user-uploaded image to a file input or drag & drop target. Supports two approaches: (1) ref - for targeting specific elements, especially hidden file inputs, (2) coordinate - for drag & drop to visible locations like Google Docs. Provide either ref or coordinate, not both."),
		mcp.WithString("imageId", mcp.Required(), mcp.Description("ID of a previously captured screenshot (from the computer tool's screenshot action) or a user-uploaded image")),
		mcp.WithString("ref", mcp.Description("Element reference ID from read_page or find tools (e.g., \"ref_1\", \"ref_2\"). Use this for file inputs (especially hidden ones) or specific elements. Provide either ref or coordinate, not both.")),
		mcp.WithArray("coordinate", mcp.Items(map[string]any{"type": "number"}), mcp.Description("Coordinates [x, y] for drag & drop to a visible location, in the coordinate frame of the most recent screenshot (like computer clicks). Use this for drag & drop targets like Google Docs. Provide either ref or coordinate, not both.")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID where the target element is located. This is where the image will be uploaded to.")),
		mcp.WithString("filename", mcp.Description("Optional filename for the uploaded file (default: \"image.png\")")),
	)
	add(uploadImageTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.uploadImage", "imageId", "ref", "coordinate", "filename")
	})

	// 9. javascript_tool
	evalTool := mcp.NewTool("javascript_tool",
		mcp.WithDescription("Execute JavaScript code in the context of the current page. The code runs in the page's context and can interact with the DOM, window object, and page variables. Returns the result of the last expression or any thrown errors. If you don't have a valid tab ID, use tabs_context first to get available tabs."),
		mcp.WithString("action", mcp.Required(), mcp.Description("Must be set to 'javascript_exec'")),
		mcp.WithString("text", mcp.Required(), mcp.Description("The JavaScript code to execute. Evaluated in the page context with REPL semantics: top-level `await` works, and the result of the last expression is returned automatically — write the expression you want (e.g. `window.myData.value`, or `await fetch(url).then(r=>r.json())`) rather than `return ...`. You can access and modify the DOM, call page functions, and interact with page variables.")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to execute the code in. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
	)
	add(evalTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.javascript", "action", "text")
	})

	// 9b. resize_window
	resizeTool := mcp.NewTool("resize_window",
		mcp.WithDescription("Resize the current browser window to specified dimensions. Useful for testing responsive designs or setting up specific screen sizes. If you don't have a valid tab ID, use tabs_context first to get available tabs."),
		mcp.WithNumber("width", mcp.Required(), mcp.Description("Target window width in pixels")),
		mcp.WithNumber("height", mcp.Required(), mcp.Description("Target window height in pixels")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to get the window for. Must be a tab in the current group. Use tabs_context first if you don't have a valid tab ID.")),
	)
	add(resizeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.resizeWindow", "width", "height")
	})

	// 9c. gif_creator
	gifTool := mcp.NewTool("gif_creator",
		mcp.WithDescription("Manage GIF recording and export for browser automation sessions. Control when to start/stop recording browser actions (clicks, scrolls, navigation), then export as an animated GIF with visual overlays (click indicators, action labels, progress bar, watermark). All operations are scoped to the tab's group. When starting recording, take a screenshot immediately after to capture the initial state as the first frame. When stopping recording, take a screenshot immediately before to capture the final state as the last frame. For export, either provide 'coordinate' to drag/drop upload to a page element, or set 'download: true' to download the GIF."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("start_recording", "stop_recording", "export", "clear"), mcp.Description("Action to perform: 'start_recording' (begin capturing), 'stop_recording' (stop capturing but keep frames), 'export' (generate and export GIF), 'clear' (discard frames)")),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("Tab ID to identify which tab group this operation applies to")),
		mcp.WithArray("coordinate", mcp.Items(map[string]any{"type": "number"}), mcp.Description("Coordinates [x, y] for drag & drop upload, in the coordinate frame of the most recent screenshot (like computer clicks). Required for 'export' action unless 'download' is true.")),
		mcp.WithBoolean("download", mcp.Description("If true, download the GIF instead of drag/drop upload. For 'export' action only.")),
		mcp.WithString("filename", mcp.Description("Optional filename for exported GIF (default: 'recording-[timestamp].gif'). For 'export' action only.")),
		mcp.WithObject("options", mcp.Description("Optional GIF enhancement options for 'export' action. Properties: showClickIndicators (bool), showDragPaths (bool), showActionLabels (bool), showProgressBar (bool), showWatermark (bool), quality (number 1-30). All default to true except quality (default: 10).")),
	)
	add(gifTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invokeTool(ctx, req, "page.gif", "action", "coordinate", "download", "filename", "options")
	})

	// 10. tabs_context_mcp
	tabsContextTool := mcp.NewTool("tabs_context_mcp",
		mcp.WithDescription("Get context information about the current MCP tab group. Returns all tab IDs inside the group if it exists. CRITICAL: You must get the context at least once before using other browser automation tools so you know what tabs exist. Each new conversation should create its own new tab (using tabs_create_mcp) rather than reusing existing tabs, unless the user explicitly asks to use an existing tab."),
		mcp.WithBoolean("createIfEmpty", mcp.Description("Creates a new MCP tab group if none exists, creates a new Window with a new tab group containing an empty tab (which can be used for this conversation). If a MCP tab group already exists, this parameter has no effect.")),
		mcp.WithString("profile", mcp.Description("Optional Chrome extension profile name (e.g. 'work', 'personal'). Required only when several profiles are online.")),
	)
	add(tabsContextTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tc, err := defaultRouter.TabsContext(ctx, req.GetString("profile", ""), req.GetBool("createIfEmpty", false))
		if err != nil {
			return mcp.NewToolResultError("Failed to query tabs: " + errorText(err)), nil
		}
		if !tc.Exists {
			return mcp.NewToolResultText("No MCP tab groups found. Any tabs opened earlier in this conversation may still be open in Chrome, outside any group these tools can see or control. Use createIfEmpty: true to create a group."), nil
		}
		return mcp.NewToolResultText(formatTabsContext(tc) + tabContextBlock(0, tc.Tabs)), nil
	})

	// 11. tabs_create_mcp
	tabsCreateTool := mcp.NewTool("tabs_create_mcp",
		mcp.WithDescription("Creates a new empty tab in the MCP tab group."),
		mcp.WithString("profile", mcp.Description("Optional Chrome extension profile name (e.g. 'work', 'personal'). Required only when several profiles are online.")),
	)
	add(tabsCreateTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabID, tc, err := defaultRouter.NewTab(ctx, req.GetString("profile", ""))
		if err != nil {
			return mcp.NewToolResultError("Failed to create tab: " + errorText(err)), nil
		}
		id, _ := strconv.Atoi(tabID)
		return mcp.NewToolResultText(fmt.Sprintf("Created new tab. Tab ID: %s", tabID) + tabContextBlock(id, tc.Tabs)), nil
	})

	// 12. tabs_close_mcp
	tabsCloseTool := mcp.NewTool("tabs_close_mcp",
		mcp.WithDescription("Close a tab in the MCP tab group by its tab ID. Use when you're done with a tab to keep the browser tidy. Only tabs in this session's group can be closed."),
		mcp.WithNumber("tabId", mcp.Required(), mcp.Description("The ID of the tab to close. Must be in this session's tab group — call tabs_context_mcp first to see valid IDs.")),
	)
	add(tabsCloseTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		closedID, err := defaultRouter.CloseTab(ctx, tabIDParam(req))
		if err != nil {
			return mcp.NewToolResultError("Failed to close tab: " + errorText(err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Closed tab %s.", closedID)), nil
	})

	// 13. browser_batch
	batchTool := mcp.NewTool("browser_batch",
		mcp.WithDescription("Execute a sequence of browser tool calls in ONE round trip. Each item is {name, input} where input is exactly what you'd pass to that tool standalone. Actions execute SEQUENTIALLY (not in parallel) and stop on the first error. Use this tool extensively to quickly execute work whenever you can predict two or more steps ahead — e.g. navigate, click a field, type, press Return, screenshot. Screenshots and other images are returned interleaved with outputs; coordinates you write in THIS batch refer to the screenshot taken BEFORE this call. browser_batch cannot be nested."),
		mcp.WithArray("actions", mcp.Required(), mcp.Items(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string", "description": "Tool name (e.g. computer, navigate, find, tabs_create_mcp). browser_batch cannot be nested."},
				"input": map[string]any{"type": "object", "description": "That tool's input — same shape you'd pass when calling it directly."},
			},
			"required": []string{"name", "input"},
		}), mcp.Description("List of tool calls to execute sequentially.")),
	)
	add(batchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runBatch(ctx, req, handlers), nil
	})

	return s
}

// tabIDParam reads the numeric `tabId` argument (Chrome's real tab id) as a decimal string.
func tabIDParam(req mcp.CallToolRequest) string {
	switch v := req.GetArguments()["tabId"].(type) {
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	case string:
		return strings.TrimSpace(v)
	}
	return ""
}

// formatTabsContext renders the tab group like Claude in Chrome (JSON, "<" and ">" escaped).
func formatTabsContext(tc TabsContext) string {
	tabs := tc.Tabs
	if tabs == nil {
		tabs = []TabSummary{}
	}
	b, _ := json.Marshal(map[string]any{"availableTabs": tabs, "tabGroupId": tc.TabGroupID})
	out := strings.NewReplacer("<", "\\u003c", ">", "\\u003e").Replace(string(b))
	return out
}

// invokeTool forwards the listed arguments to a page.* extension method and renders the response
// the way Claude in Chrome does (text, "Tab Context" block, then the image).
func invokeTool(ctx context.Context, req mcp.CallToolRequest, method string, argNames ...string) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	params := map[string]any{}
	for _, name := range argNames {
		if v, ok := args[name]; ok && v != nil {
			params[name] = v
		}
	}
	res, err := defaultRouter.Invoke(ctx, tabIDParam(req), method, params)
	if err != nil {
		return mcp.NewToolResultError(errorText(err)), nil
	}
	return renderToolResponse(res), nil
}

// errorText strips the "code: " prefix that RPC errors carry; agents only need the message.
func errorText(err error) string {
	var re *ResponseError
	if errors.As(err, &re) {
		return re.Message
	}
	return err.Error()
}

func escapeForTabContext(v string) string {
	b, _ := json.Marshal(v)
	return strings.NewReplacer("<", "\\u003c", ">", "\\u003e").Replace(string(b))
}

// tabContextBlock renders the "Tab Context" text Claude in Chrome appends to results.
func tabContextBlock(executedOn int, tabs []TabSummary) string {
	var b strings.Builder
	b.WriteString("\n\nTab Context:")
	if executedOn != 0 {
		fmt.Fprintf(&b, "\n- Executed on tabId: %d", executedOn)
	}
	b.WriteString("\n- Available tabs:")
	for _, t := range tabs {
		fmt.Fprintf(&b, "\n  • tabId %d: %s (%s)", t.TabID, escapeForTabContext(t.Title), escapeForTabContext(t.URL))
	}
	return b.String()
}

func renderToolResponse(res *ToolResponse) *mcp.CallToolResult {
	var content []mcp.Content
	if res.Output != "" {
		content = append(content, mcp.NewTextContent(res.Output))
	}
	if tc := res.TabContext; tc != nil {
		content = append(content, mcp.NewTextContent(tabContextBlock(tc.ExecutedOnTabID, tc.AvailableTabs)))
	}
	if res.Base64Image != "" {
		format := res.ImageFormat
		if format == "" {
			format = "png"
		}
		content = append(content, mcp.NewImageContent(res.Base64Image, "image/"+format))
	}
	if len(content) == 0 {
		content = append(content, mcp.NewTextContent(""))
	}
	return &mcp.CallToolResult{Content: content}
}

// runBatch executes browser_batch items sequentially (stopping on the first error) and renders
// the results like Claude in Chrome: "[tool] output" lines, images interleaved, one final Tab Context.
func runBatch(ctx context.Context, req mcp.CallToolRequest, handlers map[string]server.ToolHandlerFunc) *mcp.CallToolResult {
	items, _ := req.GetArguments()["actions"].([]any)
	if len(items) == 0 {
		return mcp.NewToolResultError("actions must be a non-empty array of {name, input}")
	}
	var content []mcp.Content
	tabContext := ""
	var lines []string
	flush := func() {
		if len(lines) > 0 {
			content = append(content, mcp.NewTextContent(strings.Join(lines, "\n")))
			lines = nil
		}
	}
	for i, raw := range items {
		item, _ := raw.(map[string]any)
		name, _ := item["name"].(string)
		input, _ := item["input"].(map[string]any)
		label := name
		if name == "computer" {
			if a, _ := input["action"].(string); a != "" {
				label = "computer:" + a
			}
		}
		fail := func(msg string) *mcp.CallToolResult {
			flush()
			content = append(content, mcp.NewTextContent(fmt.Sprintf("\n%sactions[%d] (%s) failed: %s (%d completed, %d remaining)", "", i, label, msg, i, len(items)-i-1)))
			return &mcp.CallToolResult{Content: content, IsError: true}
		}
		h, ok := handlers[name]
		if !ok || name == "browser_batch" {
			if name == "browser_batch" {
				return fail("browser_batch cannot be nested")
			}
			return fail(fmt.Sprintf("unknown tool %q", name))
		}
		var inner mcp.CallToolRequest
		inner.Params.Name = name
		inner.Params.Arguments = input
		res, err := h(ctx, inner)
		if err != nil {
			return fail(err.Error())
		}
		if res.IsError {
			msg := ""
			if len(res.Content) > 0 {
				if t, ok := res.Content[0].(mcp.TextContent); ok {
					msg = t.Text
				}
			}
			return fail(msg)
		}
		output := ""
		var images []mcp.Content
		for _, c := range res.Content {
			switch v := c.(type) {
			case mcp.TextContent:
				if strings.HasPrefix(v.Text, "\n\nTab Context:") {
					tabContext = v.Text
				} else if output == "" {
					output = v.Text
				}
			case mcp.ImageContent:
				images = append(images, v)
			}
		}
		if output == "" {
			output = "ok"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", label, output))
		if len(images) > 0 {
			flush()
			content = append(content, images...)
		}
	}
	flush()
	if tabContext != "" {
		content = append(content, mcp.NewTextContent(tabContext))
	}
	return &mcp.CallToolResult{Content: content}
}

func setupHTTPHandler(s *server.MCPServer) http.Handler {
	streamableServer := server.NewStreamableHTTPServer(s,
		server.WithStreamableHTTPCORS(),
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", streamableServer)
	mux.Handle("/sse", streamableServer)
	mux.Handle("/ext", defaultExtHub)
	mux.HandleFunc("/reload-ext", func(w http.ResponseWriter, r *http.Request) {
		for _, p := range defaultExtHub.OnlineProfiles() {
			if sess, ok := defaultExtHub.GetSession(p); ok {
				sess.Call(r.Context(), "ext.reload", nil)
			}
		}
		w.Write([]byte("reloaded"))
	})

	// Explicitly return 404 for OAuth checks so client immediately knows no auth is needed
	mux.HandleFunc("/.well-known/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	// Logging & security wrapper to trace requests and protect endpoints
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func main() {
	defaultPort := "3025"
	if p := os.Getenv("PORT"); p != "" {
		defaultPort = p
	}
	defaultHost := os.Getenv("HOST")

	_ = flag.Bool("sse", false, "Run as HTTP daemon (default; kept for backward compatibility)")
	stdioMode := flag.Bool("stdio", false, "Run as a stdio MCP server instead of the HTTP daemon (no browser extension support)")
	port := flag.String("port", defaultPort, "Port to bind HTTP/SSE server (e.g. 3025)")
	host := flag.String("host", defaultHost, "Specific host to bind (default empty: binds dual-stack loopback 127.0.0.1 and [::1])")
	tokenFlag := flag.Bool("token", false, "Print auth token for Chrome extension and exit")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	vFlag := flag.Bool("v", false, "Print version and exit")
	flag.Usage = printUsage
	flag.Parse()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help":
			printUsage()
			return
		case "version", "setup", "doctor":
		default:
			if !strings.HasPrefix(os.Args[1], "-") {
				fmt.Fprintf(os.Stderr, "Unknown command %q\n\n", os.Args[1])
				printUsage()
				os.Exit(2)
			}
		}
	}

	if *versionFlag || *vFlag || (len(os.Args) > 1 && os.Args[1] == "version") {
		fmt.Printf("klik v%s\n", Version)
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

	if len(os.Args) > 1 && os.Args[1] == "setup" {
		if err := runSetup(*port); err != nil {
			fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		runDoctor(*port)
		return
	}

	s := setupMCPServer()

	if !*stdioMode {
		// Fatal check: auth token is required for extension hub /ext endpoint
		tok, err := GetOrCreateToken()
		if err != nil || tok == "" {
			fmt.Fprintf(os.Stderr, "Fatal: failed to initialize auth token for extension hub: %v\n", err)
			os.Exit(1)
		}

		httpServer := &http.Server{
			Handler: setupHTTPHandler(s),
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

		fmt.Printf("🚀 Starting klik MCP Server (HTTP/SSE Daemon) on port %s\n", *port)
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
		fmt.Println("\n🛑 Shutting down klik MCP Server...")
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
	fmt.Printf("🩺 klik Doctor (v%s)\n\n", Version)

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
		fmt.Printf("   👉 Run 'brew services start klik' or 'make daemon' to start the server\n")
	} else {
		_ = resp.Body.Close()
		fmt.Printf("🟢 Daemon (: %s): Running and reachable at %s\n", port, daemonURL)
	}

	// 4. Chrome Extension Folder detection
	foundExt := ""
	if hasManifest(managedExtensionDir()) {
		foundExt = managedExtensionDir()
	} else {
		foundExt = findExtensionSource()
	}
	if foundExt != "" {
		fmt.Printf("🟢 Chrome Extension Folder: Found at %s\n", foundExt)
	} else {
		fmt.Printf("🟡 Chrome Extension Folder: Not detected (run 'klik setup')\n")
	}

	fmt.Printf("\n✨ Everything looks healthy! Endpoint for AI agents: http://localhost:%s/mcp\n", port)
}

func printUsage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, `klik v%s — let your AI agent use your real Chrome

Usage:
  klik <command>
  klik [flags]

Commands:
  setup      One-time setup: token, Chrome extension, register MCP in your agents
  doctor     Check that everything works and suggest fixes
  version    Print version
  help       Show this help

Common tasks:
  brew services start klik     Start the background daemon
  klik -token                  Print the auth token
  klik                         Run the daemon in the foreground (same as -port 3025)
  klik -stdio                  Stdio MCP server (for clients without HTTP support;
                                     cannot use the Chrome extension)

Running with no command starts the HTTP daemon at http://localhost:3025/mcp.

Flags:
`, Version)
	flag.PrintDefaults()
}

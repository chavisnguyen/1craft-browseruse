package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const defaultCDPURL = "http://127.0.0.1:9222"

type RodDriver struct {
	mu      sync.Mutex
	browser *rod.Browser
	pages   map[string]*rod.Page // handle -> *rod.Page
	counter int
	isCDP   bool
}

func NewRodDriver() *RodDriver {
	return &RodDriver{
		pages: make(map[string]*rod.Page),
	}
}

func (d *RodDriver) Name() string {
	return "rod"
}

func (d *RodDriver) Mode() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.isCDP {
		return "Google Chrome (CDP Port 9222)"
	}
	return "Standalone Chrome"
}

func (d *RodDriver) CheckAlive(ctx context.Context, handle string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	p, ok := d.pages[handle]
	if !ok {
		return false
	}
	if _, err := p.Info(); err != nil {
		delete(d.pages, handle)
		return false
	}
	return true
}

func (d *RodDriver) IsCDP() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.isCDP
}

func (d *RodDriver) resolveCDP(url string) string {
	if url == "none" || url == "" {
		return ""
	}
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

func (d *RodDriver) isCDPOpen(url string) bool {
	return d.resolveCDP(url) != ""
}

func (d *RodDriver) ensureBrowser() error {
	if d.browser != nil {
		if _, err := d.browser.Version(); err == nil {
			return nil
		}
		// Browser died (e.g. user quit it): drop stale handle and relaunch
		d.browser = nil
		d.pages = make(map[string]*rod.Page)
		d.isCDP = false
	}

	cdpURL := os.Getenv("CHROME_CDP_URL")
	if cdpURL == "" {
		cdpURL = defaultCDPURL
	}

	// Strategy 1: Connect to running Chrome on port 9222 (CDP)
	if ctrlURL := d.resolveCDP(cdpURL); ctrlURL != "" {
		b := rod.New().ControlURL(ctrlURL)
		if err := b.Connect(); err == nil {
			d.browser = b
			d.isCDP = true
			return nil
		}
	}

	// Strategy 2: Launch Google Chrome directly on macOS
	// Headless by default so the fallback browser never adds a second Dock icon
	headless := os.Getenv("BROWSER_HEADLESS") != "false"
	l := launcher.New().
		Bin("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome").
		Headless(headless).
		NoSandbox(true)

	controlURL, err := l.Launch()
	if err != nil {
		l = launcher.New().Headless(headless).NoSandbox(true)
		controlURL, err = l.Launch()
		if err != nil {
			return fmt.Errorf("failed to launch browser: %w", err)
		}
	}

	b := rod.New().ControlURL(controlURL)
	if err := b.Connect(); err != nil {
		return fmt.Errorf("failed to connect to launched browser: %w", err)
	}

	d.browser = b
	d.isCDP = false
	return nil
}

func (d *RodDriver) NewTab(ctx context.Context, url string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.ensureBrowser(); err != nil {
		return "", err
	}

	page := d.browser.MustPage()
	d.counter++
	handle := fmt.Sprintf("rod-handle-%d", d.counter)

	if url != "" {
		url = normalizeURL(url)
		_ = page.Navigate(url)
		_ = page.WaitLoad()
	}

	d.pages[handle] = page
	return handle, nil
}

func (d *RodDriver) getPage(handle string) (*rod.Page, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.ensureBrowser(); err != nil {
		return nil, err
	}

	p, ok := d.pages[handle]
	if !ok {
		return nil, fmt.Errorf("rod handle '%s' not found", handle)
	}

	// Check if page is still alive
	if _, err := p.Info(); err != nil {
		delete(d.pages, handle)
		return nil, fmt.Errorf("rod handle '%s' was closed", handle)
	}

	return p, nil
}

func (d *RodDriver) ListTabs(ctx context.Context, handles []string) ([]TabInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var list []TabInfo
	for _, handle := range handles {
		p, ok := d.pages[handle]
		if !ok {
			continue
		}
		title := "Unknown"
		url := "about:blank"
		if info, err := p.Info(); err == nil {
			title = info.Title
			url = info.URL
		}
		list = append(list, TabInfo{
			ID:    handle,
			Title: title,
			URL:   url,
		})
	}
	return list, nil
}

func (d *RodDriver) CloseTab(ctx context.Context, handle string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if p, ok := d.pages[handle]; ok {
		_ = p.Close()
		delete(d.pages, handle)

		// Last tab closed: shut down a browser we launched ourselves
		if len(d.pages) == 0 && !d.isCDP && d.browser != nil {
			_ = d.browser.Close()
			d.browser = nil
		}
		return nil
	}
	return fmt.Errorf("rod handle '%s' not found", handle)
}

func (d *RodDriver) Navigate(ctx context.Context, handle, url string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	url = normalizeURL(url)
	if err := p.Navigate(url); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}
	_ = p.WaitLoad()
	time.Sleep(1 * time.Second)
	return nil
}

func (d *RodDriver) Snapshot(ctx context.Context, handle string) (PageSummary, error) {
	p, err := d.getPage(handle)
	if err != nil {
		return PageSummary{}, err
	}

	info, err := p.Info()
	title := "Unknown"
	url := "Unknown"
	if err == nil {
		title = info.Title
		url = info.URL
	}

	res, err := p.Eval(jsExtractElements)
	elements := ""
	if err == nil && res.Value.Val() != nil {
		elements = res.Value.String()
	}

	return PageSummary{
		URL:      url,
		Title:    title,
		Elements: elements,
	}, nil
}

func (d *RodDriver) Click(ctx context.Context, handle, selector string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	el, err := findElement(p, selector)
	if err != nil {
		return fmt.Errorf("element not found: %w", err)
	}

	if err := el.ScrollIntoView(); err == nil {
		time.Sleep(200 * time.Millisecond)
	}

	if shape, sErr := el.Shape(); sErr == nil && shape != nil {
		cx := shape.Box().X + shape.Box().Width/2
		cy := shape.Box().Y + shape.Box().Height/2
		_, _ = p.Eval(fmt.Sprintf("(%s)(%f, %f, true)", jsShowVirtualCursor, cx, cy))
		time.Sleep(200 * time.Millisecond)
	}

	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("failed to click element: %w", err)
	}

	time.Sleep(1 * time.Second)
	return nil
}

func (d *RodDriver) Hover(ctx context.Context, handle, selector string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	el, err := findElement(p, selector)
	if err != nil {
		return fmt.Errorf("element not found: %w", err)
	}

	if err := el.ScrollIntoView(); err == nil {
		time.Sleep(200 * time.Millisecond)
	}

	if shape, sErr := el.Shape(); sErr == nil && shape != nil {
		cx := shape.Box().X + shape.Box().Width/2
		cy := shape.Box().Y + shape.Box().Height/2
		_, _ = p.Eval(fmt.Sprintf("(%s)(%f, %f, false)", jsShowVirtualCursor, cx, cy))
		_ = p.Mouse.MoveTo(proto.Point{X: cx, Y: cy})
	}

	_ = el.Hover()
	time.Sleep(300 * time.Millisecond)
	return nil
}

func (d *RodDriver) Select(ctx context.Context, handle, selector, value string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	el, err := findElement(p, selector)
	if err != nil {
		return fmt.Errorf("element not found: %w", err)
	}

	_ = el.ScrollIntoView()
	jsSelect := `(val) => {
		let sel = this;
		if (sel.tagName !== 'SELECT') sel = sel.querySelector('select') || sel.closest('select');
		if (!sel || sel.tagName !== 'SELECT') return false;
		val = (val || '').toLowerCase().trim();
		for (let opt of sel.options) {
			if (opt.value.toLowerCase() === val || opt.text.toLowerCase().includes(val)) {
				sel.value = opt.value;
				opt.selected = true;
				sel.dispatchEvent(new Event('input', { bubbles: true }));
				sel.dispatchEvent(new Event('change', { bubbles: true }));
				return true;
			}
		}
		return false;
	}`
	res, err := el.Eval(jsSelect, value)
	if err != nil {
		return fmt.Errorf("failed to select option: %w", err)
	}
	if res.Value.Val() == nil || !res.Value.Bool() {
		return fmt.Errorf("no option matching '%s' found in select element", value)
	}
	time.Sleep(200 * time.Millisecond)
	return nil
}

func (d *RodDriver) WaitFor(ctx context.Context, handle, selector string, timeoutMs int, state string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	if state == "" {
		state = "visible"
	}

	start := time.Now()
	for {
		el, err := findElement(p, selector)
		found := err == nil && el != nil
		var isVis bool
		if found {
			vis, _ := el.Visible()
			isVis = vis
		}

		if state == "attached" && found {
			return nil
		}
		if state == "visible" && isVis {
			return nil
		}
		if state == "hidden" && !isVis {
			return nil
		}

		if time.Since(start) >= time.Duration(timeoutMs)*time.Millisecond {
			return fmt.Errorf("timed out waiting %dms for '%s' to be %s", timeoutMs, selector, state)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (d *RodDriver) UploadFile(ctx context.Context, handle, selector string, files []string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("file not found: %s", f)
		}
	}

	el, err := findElement(p, selector)
	if err != nil {
		return fmt.Errorf("element not found: %w", err)
	}

	if err := el.SetFiles(files); err != nil {
		return fmt.Errorf("failed to upload files: %w", err)
	}
	return nil
}

func (d *RodDriver) DragAndDrop(ctx context.Context, handle, fromSelector, toSelector string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	fromEl, err := findElement(p, fromSelector)
	if err != nil {
		return fmt.Errorf("source element not found: %w", err)
	}
	toEl, err := findElement(p, toSelector)
	if err != nil {
		return fmt.Errorf("target element not found: %w", err)
	}

	_ = fromEl.ScrollIntoView()
	time.Sleep(100 * time.Millisecond)

	fromShape, err := fromEl.Shape()
	if err != nil {
		return fmt.Errorf("could not get source shape: %w", err)
	}
	toShape, err := toEl.Shape()
	if err != nil {
		return fmt.Errorf("could not get target shape: %w", err)
	}

	fx := fromShape.Box().X + fromShape.Box().Width/2
	fy := fromShape.Box().Y + fromShape.Box().Height/2
	tx := toShape.Box().X + toShape.Box().Width/2
	ty := toShape.Box().Y + toShape.Box().Height/2

	_, _ = p.Eval(fmt.Sprintf("(%s)(%f, %f, true)", jsShowVirtualCursor, fx, fy))
	_ = p.Mouse.MoveTo(proto.Point{X: fx, Y: fy})
	_ = p.Mouse.Down(proto.InputMouseButtonLeft, 1)

	steps := 5
	for i := 1; i <= steps; i++ {
		mx := fx + (tx-fx)*float64(i)/float64(steps)
		my := fy + (ty-fy)*float64(i)/float64(steps)
		_ = p.Mouse.MoveTo(proto.Point{X: mx, Y: my})
		time.Sleep(30 * time.Millisecond)
	}

	_ = p.Mouse.Up(proto.InputMouseButtonLeft, 1)
	_, _ = p.Eval(fmt.Sprintf("(%s)(%f, %f, false)", jsShowVirtualCursor, tx, ty))
	return nil
}

func (d *RodDriver) ActivateTab(ctx context.Context, handle string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}
	_, err = p.Activate()
	return err
}

func (d *RodDriver) ConsoleLogs(ctx context.Context, handle string, clear bool) ([]string, error) {
	p, err := d.getPage(handle)
	if err != nil {
		return nil, err
	}
	js := fmt.Sprintf(`() => {
		const logs = window.__browseruse_logs || [];
		const res = [...logs];
		if (%t) window.__browseruse_logs = [];
		return res;
	}`, clear)
	res, err := p.Eval(js)
	if err != nil || res.Value.Val() == nil {
		return []string{}, nil
	}
	var logs []string
	for _, v := range res.Value.Arr() {
		logs = append(logs, v.String())
	}
	return logs, nil
}

func (d *RodDriver) Type(ctx context.Context, handle, selector, text string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	el, err := findElement(p, selector)
	if err != nil {
		return fmt.Errorf("element not found: %w", err)
	}

	if err := el.SelectAllText(); err == nil {
		_ = el.Input("")
	}

	if err := el.Input(text); err != nil {
		return fmt.Errorf("failed to type text: %w", err)
	}

	return nil
}

func (d *RodDriver) PressKey(ctx context.Context, handle, key string) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}

	var k input.Key
	switch strings.ToLower(key) {
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

	_ = p.KeyActions().Press(k).Do()
	time.Sleep(500 * time.Millisecond)
	return nil
}

func (d *RodDriver) Scroll(ctx context.Context, handle, direction string, amount int) error {
	p, err := d.getPage(handle)
	if err != nil {
		return err
	}
	if amount <= 0 {
		amount = 500
	}
	var js string
	switch strings.ToLower(direction) {
	case "up":
		js = fmt.Sprintf("window.scrollBy({top: -%d, behavior: 'instant'})", amount)
	case "top":
		js = "window.scrollTo({top: 0, behavior: 'instant'})"
	case "bottom":
		js = "window.scrollTo({top: document.body.scrollHeight, behavior: 'instant'})"
	default:
		js = fmt.Sprintf("window.scrollBy({top: %d, behavior: 'instant'})", amount)
	}
	_, err = p.Context(ctx).Eval(fmt.Sprintf("() => { %s; return true; }", js))
	return err
}

func (d *RodDriver) Screenshot(ctx context.Context, handle string, annotate bool) ([]byte, error) {
	p, err := d.getPage(handle)
	if err != nil {
		return nil, err
	}
	if annotate {
		_, _ = p.Eval(jsAnnotateElements)
		defer func() {
			_, _ = p.Eval(jsRemoveAnnotateOverlay)
		}()
	}
	return p.Screenshot(false, nil)
}

func (d *RodDriver) Evaluate(ctx context.Context, handle, script string) (any, error) {
	p, err := d.getPage(handle)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(script)
	if !strings.HasPrefix(trimmed, "function") && !strings.HasPrefix(trimmed, "()") && !strings.Contains(trimmed, "=>") {
		trimmed = fmt.Sprintf("() => (%s)", trimmed)
	}

	res, err := p.Eval(trimmed)
	if err != nil {
		return nil, err
	}
	if res.Value.Val() != nil {
		return res.Value.Val(), nil
	}
	return res.Value, nil
}

func (d *RodDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for handle, p := range d.pages {
		_ = p.Close()
		delete(d.pages, handle)
	}
	if d.browser != nil && !d.isCDP {
		_ = d.browser.Close()
	}
	d.browser = nil
	d.isCDP = false
	return nil
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
	el, err = page.Timeout(2*time.Second).ElementR("button, a, [role='button'], [role='link']", clean)
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
	el, err = page.Timeout(2*time.Second).ElementR("*", clean)
	if err == nil && el != nil {
		return el, nil
	}

	return nil, fmt.Errorf("no element found matching '%s'", target)
}

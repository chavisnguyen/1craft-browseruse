package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ExtDriver struct {
	profile string
	hub     *ExtHub
}

func NewExtDriver(profile string, hub *ExtHub) *ExtDriver {
	return &ExtDriver{
		profile: profile,
		hub:     hub,
	}
}

func (d *ExtDriver) Name() string {
	return "ext:" + d.profile
}

func (d *ExtDriver) Mode() string {
	return fmt.Sprintf("Chrome Extension (profile '%s')", d.profile)
}

func (d *ExtDriver) session() (*ExtensionSession, error) {
	s, ok := d.hub.GetSession(d.profile)
	if !ok {
		return nil, fmt.Errorf("profile '%s' is not online", d.profile)
	}
	return s, nil
}

func parseTabHandle(handle string) (int, error) {
	tabID, err := strconv.Atoi(handle)
	if err != nil {
		return 0, fmt.Errorf("invalid tab handle '%s': %w", handle, err)
	}
	return tabID, nil
}

func (d *ExtDriver) CheckAlive(ctx context.Context, handle string) bool {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return false
	}

	s, err := d.session()
	if err != nil {
		return false
	}

	raw, err := s.Call(ctx, "tabs.list", map[string]any{
		"tabIds": []int{tabID},
	})
	if err != nil {
		return false
	}

	var res struct {
		Tabs []struct {
			TabID int `json:"tabId"`
		} `json:"tabs"`
	}
	var tabs []struct {
		TabID int `json:"tabId"`
	}
	if err := json.Unmarshal(raw, &res); err == nil && len(res.Tabs) > 0 {
		tabs = res.Tabs
	} else if err := json.Unmarshal(raw, &tabs); err != nil || len(tabs) == 0 {
		return false
	}
	return true
}

func (d *ExtDriver) NewTab(ctx context.Context, url string) (string, error) {
	s, err := d.session()
	if err != nil {
		return "", err
	}

	params := map[string]any{}
	if url != "" {
		params["url"] = normalizeURL(url)
	}

	raw, err := s.Call(ctx, "tabs.create", params)
	if err != nil {
		return "", err
	}

	var res struct {
		TabID int    `json:"tabId"`
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("invalid tabs.create response: %w", err)
	}

	return strconv.Itoa(res.TabID), nil
}

func (d *ExtDriver) ListTabs(ctx context.Context, handles []string) ([]TabInfo, error) {
	s, err := d.session()
	if err != nil {
		return nil, err
	}

	var ids []int
	for _, h := range handles {
		if id, err := strconv.Atoi(h); err == nil {
			ids = append(ids, id)
		}
	}

	raw, err := s.Call(ctx, "tabs.list", map[string]any{
		"tabIds": ids,
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		Tabs []struct {
			TabID int    `json:"tabId"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"tabs"`
	}
	var tabs []struct {
		TabID int    `json:"tabId"`
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(raw, &res); err == nil && len(res.Tabs) > 0 {
		tabs = res.Tabs
	} else if err := json.Unmarshal(raw, &tabs); err != nil {
		return nil, fmt.Errorf("invalid tabs.list response: %w", err)
	}

	var result []TabInfo
	for _, t := range tabs {
		result = append(result, TabInfo{
			ID:      strconv.Itoa(t.TabID),
			Title:   t.Title,
			URL:     t.URL,
			Profile: d.profile,
		})
	}
	return result, nil
}

func (d *ExtDriver) CloseTab(ctx context.Context, handle string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	_, err = s.Call(ctx, "tabs.close", map[string]any{
		"tabId": tabID,
	})
	return err
}

func (d *ExtDriver) Navigate(ctx context.Context, handle, url string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	_, err = s.Call(ctx, "page.navigate", map[string]any{
		"tabId": tabID,
		"url":   normalizeURL(url),
	})
	return err
}

func (d *ExtDriver) Snapshot(ctx context.Context, handle string) (PageSummary, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return PageSummary{}, err
	}

	s, err := d.session()
	if err != nil {
		return PageSummary{}, err
	}

	raw, err := s.Call(ctx, "page.snapshot", map[string]any{
		"tabId": tabID,
	})
	if err != nil {
		return PageSummary{}, err
	}

	var res struct {
		URL      string `json:"url"`
		Title    string `json:"title"`
		Elements string `json:"elements"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return PageSummary{}, fmt.Errorf("invalid page.snapshot response: %w", err)
	}

	return PageSummary{
		URL:      res.URL,
		Title:    res.Title,
		Elements: res.Elements,
	}, nil
}

func (d *ExtDriver) Click(ctx context.Context, handle, selector string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	// Always inject prominent virtual cursor with click ripple directly into the page!
	jsCursor := fmt.Sprintf(`() => {
		let el = null;
		try { el = document.querySelector(%q); } catch (e) {}
		if (!el) {
			try {
				const xp = document.evaluate(%q, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
				if (xp && xp.singleNodeValue) el = xp.singleNodeValue;
			} catch (e) {}
		}
		if (!el) {
			const clean = %q.toLowerCase().trim();
			const buttons = document.querySelectorAll('button, a, [role="button"], [role="link"], input[type="submit"]');
			for (const b of buttons) {
				if ((b.innerText || b.textContent || '').toLowerCase().includes(clean)) { el = b; break; }
			}
		}
		if (!el) return;
		el.scrollIntoView({block: 'center', inline: 'center'});
		const rect = el.getBoundingClientRect();
		const cx = rect.left + rect.width / 2;
		const cy = rect.top + rect.height / 2;
		(%s)(cx, cy, true);
	}`, selector, selector, selector, jsShowVirtualCursor)
	_, _ = d.Evaluate(ctx, handle, jsCursor)
	time.Sleep(500 * time.Millisecond) // 500ms smooth visible human-like glide and ripple animation!

	_, err = s.Call(ctx, "page.click", map[string]any{
		"tabId":    tabID,
		"selector": selector,
	})
	return err
}

func (d *ExtDriver) Type(ctx context.Context, handle, selector, text string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	// Animate virtual cursor to input box before typing
	jsCursor := fmt.Sprintf(`() => {
		let el = null;
		try { el = document.querySelector(%q); } catch (e) {}
		if (!el) {
			try {
				const xp = document.evaluate(%q, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
				if (xp && xp.singleNodeValue) el = xp.singleNodeValue;
			} catch (e) {}
		}
		if (!el) return;
		el.scrollIntoView({block: 'center', inline: 'center'});
		const rect = el.getBoundingClientRect();
		const cx = rect.left + rect.width / 2;
		const cy = rect.top + rect.height / 2;
		(%s)(cx, cy, true);
	}`, selector, selector, jsShowVirtualCursor)
	_, _ = d.Evaluate(ctx, handle, jsCursor)
	time.Sleep(150 * time.Millisecond)

	_, err = s.Call(ctx, "page.type", map[string]any{
		"tabId":    tabID,
		"selector": selector,
		"text":     text,
	})
	return err
}

func (d *ExtDriver) PressKey(ctx context.Context, handle, key string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	_, err = s.Call(ctx, "page.pressKey", map[string]any{
		"tabId": tabID,
		"key":   key,
	})
	return err
}

func (d *ExtDriver) Scroll(ctx context.Context, handle, direction string, amount int) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	if amount <= 0 {
		amount = 500
	}

	_, err = s.Call(ctx, "page.scroll", map[string]any{
		"tabId":     tabID,
		"direction": direction,
		"amount":    amount,
	})
	if err != nil {
		// Fallback via page.evaluate for backward compatibility with older extension versions
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
		_, evalErr := d.Evaluate(ctx, handle, js)
		if evalErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (d *ExtDriver) Hover(ctx context.Context, handle, selector string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	// Always animate prominent virtual cursor to element center
	jsHover := fmt.Sprintf(`() => {
		let el = null;
		try { el = document.querySelector(%q); } catch (e) {}
		if (!el) {
			try {
				const xp = document.evaluate(%q, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
				if (xp && xp.singleNodeValue) el = xp.singleNodeValue;
			} catch (e) {}
		}
		if (!el) {
			const clean = %q.toLowerCase().trim();
			const buttons = document.querySelectorAll('button, a, [role="button"], [role="link"]');
			for (const b of buttons) {
				if ((b.innerText || b.textContent || '').toLowerCase().includes(clean)) { el = b; break; }
			}
		}
		if (!el) return false;
		el.scrollIntoView({block: 'center', inline: 'center'});
		const rect = el.getBoundingClientRect();
		const cx = rect.left + rect.width / 2;
		const cy = rect.top + rect.height / 2;
		(%s)(cx, cy, false);
		el.dispatchEvent(new MouseEvent('mouseover', {bubbles: true, clientX: cx, clientY: cy}));
		el.dispatchEvent(new MouseEvent('mouseenter', {bubbles: true, clientX: cx, clientY: cy}));
		return true;
	}`, selector, selector, selector, jsShowVirtualCursor)
	_, _ = d.Evaluate(ctx, handle, jsHover)
	time.Sleep(500 * time.Millisecond) // 500ms smooth visible human-like glide!

	_, err = s.Call(ctx, "page.hover", map[string]any{
		"tabId":    tabID,
		"selector": selector,
	})
	if err != nil {
		return nil // already handled by evaluate
	}
	return nil
}

func (d *ExtDriver) Select(ctx context.Context, handle, selector, value string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	_, err = s.Call(ctx, "page.select", map[string]any{
		"tabId":    tabID,
		"selector": selector,
		"value":    value,
	})
	if err != nil {
		// Defensive fallback via page.evaluate
		jsSelect := fmt.Sprintf(`() => {
			let sel = document.querySelector(%q);
			if (!sel) return false;
			if (sel.tagName !== 'SELECT') sel = sel.querySelector('select') || sel.closest('select');
			if (!sel || sel.tagName !== 'SELECT') return false;
			const val = %q.toLowerCase().trim();
			for (let opt of sel.options) {
				if (opt.value.toLowerCase() === val || opt.text.toLowerCase().includes(val)) {
					sel.value = opt.value;
					opt.selected = true;
					sel.dispatchEvent(new Event('input', {bubbles: true}));
					sel.dispatchEvent(new Event('change', {bubbles: true}));
					return true;
				}
			}
			return false;
		}`, selector, value)
		if _, evalErr := d.Evaluate(ctx, handle, jsSelect); evalErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (d *ExtDriver) WaitFor(ctx context.Context, handle, selector string, timeoutMs int, state string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	if state == "" {
		state = "visible"
	}

	_, err = s.Call(ctx, "page.waitFor", map[string]any{
		"tabId":     tabID,
		"selector":  selector,
		"timeoutMs": timeoutMs,
		"state":     state,
	})
	if err != nil {
		// Defensive fallback via page.evaluate polling
		jsWait := fmt.Sprintf(`() => {
			const el = document.querySelector(%q);
			const state = %q;
			const isAttached = el !== null;
			let isVis = false;
			if (isAttached) {
				const rect = el.getBoundingClientRect();
				const style = window.getComputedStyle(el);
				isVis = rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden';
			}
			if (state === 'attached') return isAttached;
			if (state === 'hidden') return !isVis;
			return isVis;
		}`, selector, state)
		start := time.Now()
		for {
			res, evalErr := d.Evaluate(ctx, handle, jsWait)
			if evalErr == nil {
				if b, ok := res.(bool); ok && b {
					return nil
				}
			}
			if time.Since(start) >= time.Duration(timeoutMs)*time.Millisecond {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		return err
	}
	return nil
}

func (d *ExtDriver) UploadFile(ctx context.Context, handle, selector string, files []string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	_, err = s.Call(ctx, "page.uploadFile", map[string]any{
		"tabId":    tabID,
		"selector": selector,
		"files":    files,
	})
	return err
}

func (d *ExtDriver) DragAndDrop(ctx context.Context, handle, fromSelector, toSelector string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	_, err = s.Call(ctx, "page.dragAndDrop", map[string]any{
		"tabId": tabID,
		"from":  fromSelector,
		"to":    toSelector,
	})
	return err
}

func (d *ExtDriver) ActivateTab(ctx context.Context, handle string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	_, err = s.Call(ctx, "page.activateTab", map[string]any{
		"tabId": tabID,
	})
	return err
}

func (d *ExtDriver) ConsoleLogs(ctx context.Context, handle string, clear bool) ([]string, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	raw, err := s.Call(ctx, "page.consoleLogs", map[string]any{
		"tabId": tabID,
		"clear": clear,
	})
	if err != nil {
		// Defensive fallback via page.evaluate
		jsLogs := fmt.Sprintf(`() => {
			const logs = window.__browseruse_logs || [];
			const res = [...logs];
			if (%t) window.__browseruse_logs = [];
			return res;
		}`, clear)
		res, evalErr := d.Evaluate(ctx, handle, jsLogs)
		if evalErr == nil {
			if arr, ok := res.([]any); ok {
				var logs []string
				for _, item := range arr {
					logs = append(logs, fmt.Sprint(item))
				}
				return logs, nil
			}
		}
		return []string{}, nil
	}

	var res struct {
		Logs []string `json:"logs"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("invalid page.consoleLogs response: %w", err)
	}

	return res.Logs, nil
}

func (d *ExtDriver) Screenshot(ctx context.Context, handle string, annotate bool) ([]byte, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	if annotate {
		_, _ = d.Evaluate(ctx, handle, jsAnnotateElements)
		defer func() {
			_, _ = d.Evaluate(ctx, handle, jsRemoveAnnotateOverlay)
		}()
	}

	raw, err := s.Call(ctx, "page.screenshot", map[string]any{
		"tabId":    tabID,
		"annotate": annotate,
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		PNGBase64 string `json:"pngBase64"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("invalid page.screenshot response: %w", err)
	}

	return base64.StdEncoding.DecodeString(res.PNGBase64)
}

func (d *ExtDriver) Evaluate(ctx context.Context, handle, script string) (any, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	raw, err := s.Call(ctx, "page.evaluate", map[string]any{
		"tabId":  tabID,
		"script": script,
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("invalid page.evaluate response: %w", err)
	}

	return res.Value, nil
}

func (d *ExtDriver) Close() error {
	return nil
}

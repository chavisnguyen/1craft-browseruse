package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
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
		TabID int `json:"tabId"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("failed to parse tabs.create response: %w", err)
	}

	return strconv.Itoa(res.TabID), nil
}

func (d *ExtDriver) ListTabs(ctx context.Context, handles []string) ([]TabInfo, error) {
	s, err := d.session()
	if err != nil {
		return nil, err
	}

	params := map[string]any{}
	if len(handles) > 0 {
		var ids []int
		for _, h := range handles {
			if id, err := parseTabHandle(h); err == nil {
				ids = append(ids, id)
			}
		}
		params["tabIds"] = ids
	}

	raw, err := s.Call(ctx, "tabs.context", params)
	if err != nil {
		raw, err = s.Call(ctx, "tabs.list", params)
		if err != nil {
			return nil, err
		}
	}

	var res struct {
		Tabs []struct {
			TabID   int    `json:"tabId"`
			Title   string `json:"title"`
			URL     string `json:"url"`
			Active  bool   `json:"active"`
			GroupID *int   `json:"groupId"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("failed to parse tabs response: %w", err)
	}

	var out []TabInfo
	for _, t := range res.Tabs {
		out = append(out, TabInfo{
			ID:      strconv.Itoa(t.TabID),
			Title:   t.Title,
			URL:     t.URL,
			Profile: d.profile,
			Active:  t.Active,
			GroupID: t.GroupID,
		})
	}
	return out, nil
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

func (d *ExtDriver) Navigate(ctx context.Context, handle, url, action string) error {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return err
	}

	s, err := d.session()
	if err != nil {
		return err
	}

	params := map[string]any{
		"tabId": tabID,
	}
	if action != "" {
		params["action"] = action
	}
	if url != "" {
		params["url"] = normalizeURL(url)
	}

	_, err = s.Call(ctx, "page.navigate", params)
	return err
}

func (d *ExtDriver) ReadPage(ctx context.Context, handle, filter string) (PageSummary, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return PageSummary{}, err
	}

	s, err := d.session()
	if err != nil {
		return PageSummary{}, err
	}

	params := map[string]any{
		"tabId": tabID,
	}
	if filter != "" {
		params["filter"] = filter
	}

	raw, err := s.Call(ctx, "page.readPage", params)
	if err != nil {
		// Fallback to snapshot
		raw, err = s.Call(ctx, "page.snapshot", params)
		if err != nil {
			return PageSummary{}, err
		}
	}

	var res struct {
		URL      string `json:"url"`
		Title    string `json:"title"`
		Elements string `json:"elements"`
		Count    int    `json:"count"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return PageSummary{}, fmt.Errorf("failed to parse readPage response: %w", err)
	}

	return PageSummary{
		URL:      res.URL,
		Title:    res.Title,
		Elements: res.Elements,
		Count:    res.Count,
	}, nil
}

func (d *ExtDriver) Computer(ctx context.Context, handle, action string, params map[string]any) (any, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	p := map[string]any{
		"tabId":  tabID,
		"action": action,
	}
	for k, v := range params {
		p[k] = v
	}

	raw, err := s.Call(ctx, "page.computer", p)
	if err != nil {
		return nil, err
	}

	var res any
	_ = json.Unmarshal(raw, &res)
	return res, nil
}

func (d *ExtDriver) FormInput(ctx context.Context, handle, refID string, value any, checked *bool) (any, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	p := map[string]any{
		"tabId": tabID,
		"refId": refID,
	}
	if value != nil {
		p["value"] = value
	}
	if checked != nil {
		p["checked"] = *checked
	}

	raw, err := s.Call(ctx, "page.formInput", p)
	if err != nil {
		return nil, err
	}

	var res any
	_ = json.Unmarshal(raw, &res)
	return res, nil
}

func (d *ExtDriver) Find(ctx context.Context, handle, query string) (any, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	raw, err := s.Call(ctx, "page.find", map[string]any{
		"tabId": tabID,
		"query": query,
	})
	if err != nil {
		return nil, err
	}

	var res any
	_ = json.Unmarshal(raw, &res)
	return res, nil
}

func (d *ExtDriver) ReadNetworkRequests(ctx context.Context, handle string, clear bool, filter string) (any, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	raw, err := s.Call(ctx, "page.readNetworkRequests", map[string]any{
		"tabId":  tabID,
		"clear":  clear,
		"filter": filter,
	})
	if err != nil {
		return nil, err
	}

	var res any
	_ = json.Unmarshal(raw, &res)
	return res, nil
}

func (d *ExtDriver) ReadConsoleMessages(ctx context.Context, handle string, clear bool) ([]string, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	raw, err := s.Call(ctx, "page.readConsoleMessages", map[string]any{
		"tabId": tabID,
		"clear": clear,
	})
	if err != nil {
		// Fallback to page.consoleLogs
		raw, err = s.Call(ctx, "page.consoleLogs", map[string]any{
			"tabId": tabID,
			"clear": clear,
		})
		if err != nil {
			return nil, err
		}
	}

	var res struct {
		Logs []string `json:"logs"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("failed to parse console logs: %w", err)
	}

	return res.Logs, nil
}

func (d *ExtDriver) BrowserBatch(ctx context.Context, handle string, actions []any) (any, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	raw, err := s.Call(ctx, "page.browserBatch", map[string]any{
		"tabId":   tabID,
		"actions": actions,
	})
	if err != nil {
		return nil, err
	}

	var res any
	_ = json.Unmarshal(raw, &res)
	return res, nil
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
		return nil, fmt.Errorf("failed to parse evaluate response: %w", err)
	}

	return res.Value, nil
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

	raw, err := s.Call(ctx, "page.computer", map[string]any{
		"tabId":  tabID,
		"action": "screenshot",
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		PNGBase64 string `json:"pngBase64"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.PNGBase64 == "" {
		return nil, fmt.Errorf("failed to capture screenshot: empty data")
	}

	return base64.StdEncoding.DecodeString(res.PNGBase64)
}

func (d *ExtDriver) Close() error {
	return nil
}

// Compatibility helpers
func (d *ExtDriver) Snapshot(ctx context.Context, handle string) (PageSummary, error) {
	return d.ReadPage(ctx, handle, "interactive")
}

func (d *ExtDriver) Click(ctx context.Context, handle, selector string) error {
	_, err := d.Computer(ctx, handle, "left_click", map[string]any{"selector": selector})
	return err
}

func (d *ExtDriver) Hover(ctx context.Context, handle, selector string) error {
	_, err := d.Computer(ctx, handle, "hover", map[string]any{"selector": selector})
	return err
}

func (d *ExtDriver) Type(ctx context.Context, handle, selector, text string) error {
	_, err := d.Computer(ctx, handle, "type", map[string]any{"selector": selector, "text": text})
	return err
}

func (d *ExtDriver) PressKey(ctx context.Context, handle, key string) error {
	_, err := d.Computer(ctx, handle, "key", map[string]any{"key": key})
	return err
}

func (d *ExtDriver) Select(ctx context.Context, handle, selector, value string) error {
	_, err := d.FormInput(ctx, handle, selector, value, nil)
	return err
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
	_, err = s.Call(ctx, "page.waitFor", map[string]any{
		"tabId":     tabID,
		"selector":  selector,
		"timeoutMs": timeoutMs,
		"state":     state,
	})
	return err
}

func (d *ExtDriver) DragAndDrop(ctx context.Context, handle, fromSelector, toSelector string) error {
	_, err := d.Computer(ctx, handle, "drag", map[string]any{"from": fromSelector, "to": toSelector})
	return err
}

func (d *ExtDriver) Scroll(ctx context.Context, handle, direction string, amount int) error {
	_, err := d.Computer(ctx, handle, "scroll", map[string]any{"direction": direction, "amount": amount})
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
	_, err = s.Call(ctx, "page.activateTab", map[string]any{"tabId": tabID})
	return err
}

func (d *ExtDriver) ConsoleLogs(ctx context.Context, handle string, clear bool) ([]string, error) {
	return d.ReadConsoleMessages(ctx, handle, clear)
}

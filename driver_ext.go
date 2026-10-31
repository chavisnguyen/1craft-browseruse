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

func (d *ExtDriver) Screenshot(ctx context.Context, handle string) ([]byte, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}

	s, err := d.session()
	if err != nil {
		return nil, err
	}

	raw, err := s.Call(ctx, "page.screenshot", map[string]any{
		"tabId": tabID,
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

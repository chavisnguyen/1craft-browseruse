package main

import (
	"context"
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

func (d *ExtDriver) NewTab(ctx context.Context) (string, error) {
	s, err := d.session()
	if err != nil {
		return "", err
	}

	raw, err := s.Call(ctx, "tabs.create", map[string]any{})
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

func (d *ExtDriver) TabsContext(ctx context.Context, createIfEmpty bool) (TabsContext, error) {
	s, err := d.session()
	if err != nil {
		return TabsContext{}, err
	}

	raw, err := s.Call(ctx, "tabs.context", map[string]any{"createIfEmpty": createIfEmpty})
	if err != nil {
		return TabsContext{}, err
	}

	var res struct {
		Exists        bool         `json:"exists"`
		TabGroupID    int          `json:"tabGroupId"`
		AvailableTabs []TabSummary `json:"availableTabs"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return TabsContext{}, fmt.Errorf("failed to parse tabs.context response: %w", err)
	}
	return TabsContext{Exists: res.Exists, TabGroupID: res.TabGroupID, Tabs: res.AvailableTabs}, nil
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

func (d *ExtDriver) Close() error {
	return nil
}

// Invoke calls a page.* method on the tab and decodes the generic tool response.
func (d *ExtDriver) Invoke(ctx context.Context, handle, method string, params map[string]any) (*ToolResponse, error) {
	tabID, err := parseTabHandle(handle)
	if err != nil {
		return nil, err
	}
	s, err := d.session()
	if err != nil {
		return nil, err
	}
	p := map[string]any{"tabId": tabID}
	for k, v := range params {
		p[k] = v
	}
	raw, err := s.Call(ctx, method, p)
	if err != nil {
		return nil, err
	}
	var res ToolResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", method, err)
	}
	return &res, nil
}

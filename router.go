package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type tabRef struct {
	tabID   string
	driver  Driver
	handle  string
	profile string
}

type Router struct {
	mu          sync.Mutex
	tabs        map[string]tabRef
	hub         *ExtHub
	lastCleanup time.Time
}

func NewRouter(hub *ExtHub) *Router {
	r := &Router{
		tabs: make(map[string]tabRef),
		hub:  hub,
	}
	if hub != nil {
		hub.SetRouter(r)
	}
	return r
}

// selectDriverLocked resolves the backend for a profile request. It must be called with r.mu held.
func (r *Router) selectDriverLocked(profile string) (Driver, string, error) {
	if profile != "" {
		if r.hub == nil {
			return nil, "", fmt.Errorf("profile '%s' is not online", profile)
		}
		if _, ok := r.hub.GetSession(profile); !ok {
			return nil, "", fmt.Errorf("profile '%s' is not online", profile)
		}
		return NewExtDriver(profile, r.hub), profile, nil
	}

	var online []string
	if r.hub != nil {
		online = r.hub.OnlineProfiles()
		sort.Strings(online)
	}
	switch len(online) {
	case 0:
		return nil, "", fmt.Errorf("no browser extension is online. Install the klik Chrome extension (run 'klik setup') and open Chrome")
	case 1:
		return NewExtDriver(online[0], r.hub), online[0], nil
	default:
		return nil, "", fmt.Errorf("multiple extension profiles online (%s). Please specify 'profile' (in tabs_context_mcp or tabs_create_mcp)", strings.Join(online, ", "))
	}
}

// registerTabLocked remembers which driver/profile owns a Chrome tab id.
func (r *Router) registerTabLocked(tabID string, driver Driver, profile string) tabRef {
	ref := tabRef{tabID: tabID, driver: driver, handle: tabID, profile: profile}
	r.tabs[tabID] = ref
	return ref
}

// TabsContext implements tabs_context_mcp: returns (and optionally creates) the MCP tab group.
func (r *Router) TabsContext(ctx context.Context, profile string, createIfEmpty bool) (TabsContext, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	driver, selectedProfile, err := r.selectDriverLocked(profile)
	if err != nil {
		return TabsContext{}, err
	}
	tc, err := driver.TabsContext(ctx, createIfEmpty)
	if err != nil {
		return TabsContext{}, err
	}
	for _, t := range tc.Tabs {
		r.registerTabLocked(strconv.Itoa(t.TabID), driver, selectedProfile)
	}
	return tc, nil
}

// NewTab implements tabs_create_mcp: creates an empty tab inside the MCP tab group.
func (r *Router) NewTab(ctx context.Context, profile string) (string, TabsContext, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	driver, selectedProfile, err := r.selectDriverLocked(profile)
	if err != nil {
		return "", TabsContext{}, err
	}
	handle, err := driver.NewTab(ctx)
	if err != nil {
		return "", TabsContext{}, err
	}
	r.registerTabLocked(handle, driver, selectedProfile)

	tc, err := driver.TabsContext(ctx, false)
	if err != nil {
		return handle, TabsContext{}, nil
	}
	return handle, tc, nil
}

func (r *Router) cleanupDeadTabsLocked(ctx context.Context) {
	for id, ref := range r.tabs {
		if !ref.driver.CheckAlive(ctx, ref.handle) {
			delete(r.tabs, id)
		}
	}
}

// getTabRefLocked resolves a Chrome tab id (as a decimal string) to its owning driver.
// Unknown ids are looked up on every online extension profile.
func (r *Router) getTabRefLocked(ctx context.Context, tabID string) (tabRef, error) {
	if tabID == "" {
		return tabRef{}, fmt.Errorf("tabId is required. Call tabs_context_mcp first to get valid tab IDs")
	}
	if ref, ok := r.tabs[tabID]; ok {
		if ref.driver.CheckAlive(ctx, ref.handle) {
			return ref, nil
		}
		delete(r.tabs, tabID)
	}
	if r.hub != nil {
		profiles := r.hub.OnlineProfiles()
		sort.Strings(profiles)
		for _, profile := range profiles {
			driver := NewExtDriver(profile, r.hub)
			if driver.CheckAlive(ctx, tabID) {
				return r.registerTabLocked(tabID, driver, profile), nil
			}
		}
	}
	return tabRef{}, fmt.Errorf("Tab %s does not exist (may have already been closed). Call tabs_context_mcp to see current tabs", tabID)
}

func (r *Router) GetTab(ctx context.Context, tabID string) (tabRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getTabRefLocked(ctx, tabID)
}

// CloseTab implements tabs_close_mcp. The extension enforces tab-group confinement.
func (r *Router) CloseTab(ctx context.Context, tabID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ref, err := r.getTabRefLocked(ctx, tabID)
	if err != nil {
		return tabID, err
	}
	if err := ref.driver.CloseTab(ctx, ref.handle); err != nil {
		return tabID, err
	}
	delete(r.tabs, tabID)
	return tabID, nil
}

func (r *Router) CloseAll(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tabs = make(map[string]tabRef)
}

func (r *Router) onProfileDisconnected(profile string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, ref := range r.tabs {
		if ref.profile == profile {
			delete(r.tabs, id)
		}
	}
}

func (r *Router) countTabsForProfile(profile string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	count := 0
	for _, ref := range r.tabs {
		if ref.profile == profile {
			count++
		}
	}
	return count
}

// Invoke runs a page.* tool call on a tab (tabID is Chrome's numeric tab id as a string).
func (r *Router) Invoke(ctx context.Context, tabID, method string, params map[string]any) (*ToolResponse, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return ref.driver.Invoke(ctx, ref.handle, method, params)
}

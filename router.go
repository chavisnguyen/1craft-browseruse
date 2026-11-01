package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	mu        sync.Mutex
	tabs      map[string]tabRef
	activeTab string
	counter   int
	rod       *RodDriver
	hub       *ExtHub
}

func NewRouter(rod *RodDriver, hub *ExtHub) *Router {
	r := &Router{
		tabs: make(map[string]tabRef),
		rod:  rod,
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
		// Fallback to rod backend
		return r.rod, "", nil
	case 1:
		return NewExtDriver(online[0], r.hub), online[0], nil
	default:
		return nil, "", fmt.Errorf("multiple extension profiles online (%s). Please specify 'profile' in 'browser_new_tab'", strings.Join(online, ", "))
	}
}

func (r *Router) NewTab(ctx context.Context, url string, profile string) (string, PageSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	driver, selectedProfile, err := r.selectDriverLocked(profile)
	if err != nil {
		return "", PageSummary{}, err
	}

	handle, err := driver.NewTab(ctx, url)
	if err != nil {
		return "", PageSummary{}, err
	}

	r.counter++
	tabID := fmt.Sprintf("tab-%d", r.counter)

	summary, err := driver.Snapshot(ctx, handle)
	if err != nil {
		// Even if initial snapshot fails, tab was created
		summary = PageSummary{URL: url, Title: "New Tab"}
	}

	ref := tabRef{
		tabID:   tabID,
		driver:  driver,
		handle:  handle,
		profile: selectedProfile,
	}

	r.tabs[tabID] = ref
	r.activeTab = tabID

	return tabID, summary, nil
}

func (r *Router) cleanupDeadTabsLocked(ctx context.Context) {
	for id, ref := range r.tabs {
		if !ref.driver.CheckAlive(ctx, ref.handle) {
			delete(r.tabs, id)
			if r.activeTab == id {
				r.activeTab = ""
			}
		}
	}
}

// getTabRefLocked returns tabRef for tabID or active tab without locking.
// It first prunes any closed/dead tabs. If no tab exists at all, it auto-creates the first tab.
func (r *Router) getTabRefLocked(ctx context.Context, tabID string) (tabRef, error) {
	// Clean up any dead tabs first (e.g. closed by user or browser restart)
	r.cleanupDeadTabsLocked(ctx)

	// 1. Explicit tabID provided
	if tabID != "" {
		ref, ok := r.tabs[tabID]
		if ok {
			return ref, nil
		}
		return tabRef{}, fmt.Errorf("tab '%s' not found or was closed. Call 'browser_list_tabs' or 'browser_new_tab'", tabID)
	}

	// 2. Active tab
	if r.activeTab != "" {
		if ref, ok := r.tabs[r.activeTab]; ok {
			return ref, nil
		}
	}

	// 3. Fallback to any existing tab
	if len(r.tabs) > 0 {
		for id, ref := range r.tabs {
			r.activeTab = id
			return ref, nil
		}
	}

	// 4. Create the first tab using profile selection rules
	driver, selectedProfile, err := r.selectDriverLocked("")
	if err != nil {
		return tabRef{}, err
	}
	handle, err := driver.NewTab(ctx, "")
	if err != nil {
		return tabRef{}, err
	}

	r.counter++
	newTabID := fmt.Sprintf("tab-%d", r.counter)
	ref := tabRef{
		tabID:   newTabID,
		driver:  driver,
		handle:  handle,
		profile: selectedProfile,
	}
	r.tabs[newTabID] = ref
	r.activeTab = newTabID
	return ref, nil
}

func (r *Router) GetTab(ctx context.Context, tabID string) (tabRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getTabRefLocked(ctx, tabID)
}

func (r *Router) CloseTab(ctx context.Context, tabID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	targetID := tabID
	if targetID == "" {
		targetID = r.activeTab
	}

	ref, ok := r.tabs[targetID]
	if !ok {
		return targetID, fmt.Errorf("tab '%s' not found", targetID)
	}

	_ = ref.driver.CloseTab(ctx, ref.handle)
	delete(r.tabs, targetID)

	if r.activeTab == targetID {
		r.activeTab = ""
		for id := range r.tabs {
			r.activeTab = id
			break
		}
	}

	return targetID, nil
}

func (r *Router) ListTabs(ctx context.Context) ([]TabInfo, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cleanupDeadTabsLocked(ctx)

	var result []TabInfo
	for id, ref := range r.tabs {
		infos, err := ref.driver.ListTabs(ctx, []string{ref.handle})
		if err != nil || len(infos) == 0 {
			continue
		}
		info := infos[0]
		info.ID = id
		info.Profile = ref.profile
		result = append(result, info)
	}

	if r.activeTab == "" && len(r.tabs) > 0 {
		for id := range r.tabs {
			r.activeTab = id
			break
		}
	}

	return result, r.activeTab
}

func (r *Router) CloseAll(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ref := range r.tabs {
		_ = ref.driver.CloseTab(ctx, ref.handle)
	}
	r.tabs = make(map[string]tabRef)
	r.activeTab = ""
	_ = r.rod.Close()
}

func (r *Router) Navigate(ctx context.Context, tabID, url string) (string, string, PageSummary, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", "", PageSummary{}, err
	}

	if err := ref.driver.Navigate(ctx, ref.handle, url); err != nil {
		return ref.tabID, "", PageSummary{}, err
	}

	mode := ref.driver.Mode()

	summary, err := ref.driver.Snapshot(ctx, ref.handle)
	if err != nil {
		return ref.tabID, mode, PageSummary{}, err
	}

	return ref.tabID, mode, summary, nil
}

func (r *Router) Snapshot(ctx context.Context, tabID string) (string, PageSummary, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", PageSummary{}, err
	}

	summary, err := ref.driver.Snapshot(ctx, ref.handle)
	if err != nil {
		return ref.tabID, PageSummary{}, err
	}
	return ref.tabID, summary, nil
}

func (r *Router) Click(ctx context.Context, tabID, selector string) (string, PageSummary, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", PageSummary{}, err
	}

	if err := ref.driver.Click(ctx, ref.handle, selector); err != nil {
		return ref.tabID, PageSummary{}, err
	}

	summary, err := ref.driver.Snapshot(ctx, ref.handle)
	if err != nil {
		return ref.tabID, PageSummary{}, err
	}
	return ref.tabID, summary, nil
}

func (r *Router) Hover(ctx context.Context, tabID, selector string) (string, PageSummary, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", PageSummary{}, err
	}

	if err := ref.driver.Hover(ctx, ref.handle, selector); err != nil {
		return ref.tabID, PageSummary{}, err
	}

	summary, err := ref.driver.Snapshot(ctx, ref.handle)
	if err != nil {
		return ref.tabID, PageSummary{}, err
	}
	return ref.tabID, summary, nil
}

func (r *Router) Select(ctx context.Context, tabID, selector, value string) (string, PageSummary, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", PageSummary{}, err
	}

	if err := ref.driver.Select(ctx, ref.handle, selector, value); err != nil {
		return ref.tabID, PageSummary{}, err
	}

	summary, err := ref.driver.Snapshot(ctx, ref.handle)
	if err != nil {
		return ref.tabID, PageSummary{}, err
	}
	return ref.tabID, summary, nil
}

func (r *Router) WaitFor(ctx context.Context, tabID, selector string, timeoutMs int, state string) (string, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", err
	}

	if err := ref.driver.WaitFor(ctx, ref.handle, selector, timeoutMs, state); err != nil {
		return ref.tabID, err
	}
	return ref.tabID, nil
}

func (r *Router) UploadFile(ctx context.Context, tabID, selector string, files []string) (string, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", err
	}

	if err := ref.driver.UploadFile(ctx, ref.handle, selector, files); err != nil {
		return ref.tabID, err
	}
	return ref.tabID, nil
}

func (r *Router) DragAndDrop(ctx context.Context, tabID, fromSelector, toSelector string) (string, PageSummary, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", PageSummary{}, err
	}

	if err := ref.driver.DragAndDrop(ctx, ref.handle, fromSelector, toSelector); err != nil {
		return ref.tabID, PageSummary{}, err
	}

	summary, err := ref.driver.Snapshot(ctx, ref.handle)
	if err != nil {
		return ref.tabID, PageSummary{}, err
	}
	return ref.tabID, summary, nil
}

func (r *Router) ActivateTab(ctx context.Context, tabID string) (string, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", err
	}

	if err := ref.driver.ActivateTab(ctx, ref.handle); err != nil {
		return ref.tabID, err
	}
	return ref.tabID, nil
}

func (r *Router) ConsoleLogs(ctx context.Context, tabID string, clear bool) (string, []string, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", nil, err
	}

	logs, err := ref.driver.ConsoleLogs(ctx, ref.handle, clear)
	if err != nil {
		return ref.tabID, nil, err
	}
	return ref.tabID, logs, nil
}

func (r *Router) Type(ctx context.Context, tabID, selector, text string, pressEnter bool) (string, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", err
	}

	if err := ref.driver.Type(ctx, ref.handle, selector, text); err != nil {
		return ref.tabID, err
	}

	if pressEnter {
		_ = ref.driver.PressKey(ctx, ref.handle, "enter")
		time.Sleep(1 * time.Second)
	}

	return ref.tabID, nil
}

func (r *Router) PressKey(ctx context.Context, tabID, key string) (string, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", err
	}

	if err := ref.driver.PressKey(ctx, ref.handle, key); err != nil {
		return ref.tabID, err
	}
	return ref.tabID, nil
}

func (r *Router) Scroll(ctx context.Context, tabID, direction string, amount int) (string, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", err
	}

	if err := ref.driver.Scroll(ctx, ref.handle, direction, amount); err != nil {
		return ref.tabID, err
	}
	return ref.tabID, nil
}

func (r *Router) Screenshot(ctx context.Context, tabID, filename string, annotate bool) (string, string, error) {
	if filename == "" {
		filename = "screenshot.png"
	}
	if !filepath.IsAbs(filename) && !strings.HasPrefix(filename, "/") {
		filename = "/tmp/" + filename
	}

	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", "", err
	}

	data, err := ref.driver.Screenshot(ctx, ref.handle, annotate)
	if err != nil {
		return ref.tabID, "", fmt.Errorf("failed to take screenshot: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return ref.tabID, "", fmt.Errorf("failed to save screenshot: %w", err)
	}

	return ref.tabID, filename, nil
}

func (r *Router) Evaluate(ctx context.Context, tabID, script string) (string, any, error) {
	r.mu.Lock()
	ref, err := r.getTabRefLocked(ctx, tabID)
	r.mu.Unlock()
	if err != nil {
		return "", nil, err
	}

	val, err := ref.driver.Evaluate(ctx, ref.handle, script)
	if err != nil {
		return ref.tabID, nil, err
	}
	return ref.tabID, val, nil
}

func (r *Router) onProfileDisconnected(profile string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, ref := range r.tabs {
		if ref.profile == profile {
			delete(r.tabs, id)
			if r.activeTab == id {
				r.activeTab = ""
			}
		}
	}

	if r.activeTab == "" && len(r.tabs) > 0 {
		for id := range r.tabs {
			r.activeTab = id
			break
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

func (r *Router) GetStatus(ctx context.Context) string {
	cdpURL := os.Getenv("CHROME_CDP_URL")
	if cdpURL == "" {
		cdpURL = defaultCDPURL
	}
	isOpen := r.rod.isCDPOpen(cdpURL)

	status := "### Browser Automation Status (Golang Multi-Agent Engine)\n"
	if isOpen {
		status += "- **Chrome CDP (Port 9222)**: 🟢 Connected to Google Chrome\n"
		status += "- **Mode**: CDP Attached (Shared session & zero extra RAM)\n"
	} else {
		status += "- **Chrome CDP (Port 9222)**: 🔴 Not connected (Using standalone Chrome)\n"
		status += "- **Mode**: Standalone Chrome\n"
		status += "\n💡 **Tip**: Run `make chrome` to attach to your real Google Chrome!\n"
	}

	if r.hub != nil {
		profiles := r.hub.ListProfiles()
		status += fmt.Sprintf("- **Extension Profiles Online**: %d\n", len(profiles))
		for _, p := range profiles {
			emailInfo := ""
			if p.Email != "" {
				emailInfo = fmt.Sprintf(" (%s)", p.Email)
			}
			status += fmt.Sprintf("  * `[%s]`%s — %d tabs managed (connected %s)\n",
				p.Profile, emailInfo, p.OpenTabs, p.ConnectedAt.Format("15:04:05"))
		}
	}

	tabs, _ := r.ListTabs(ctx)
	status += fmt.Sprintf("- **Open Tabs Managed**: %d\n", len(tabs))
	for _, t := range tabs {
		profileTag := ""
		if t.Profile != "" {
			profileTag = fmt.Sprintf(" [%s]", t.Profile)
		}
		status += fmt.Sprintf("  * `[%s]`%s %s (`%s`)\n", t.ID, profileTag, t.Title, t.URL)
	}
	return status
}

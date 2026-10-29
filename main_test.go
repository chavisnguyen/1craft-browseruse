package main

import (
	"os"
	"strings"
	"testing"
)

func TestExtractElementsJS(t *testing.T) {
	if !strings.Contains(jsExtractElements, "querySelectorAll") {
		t.Fatalf("expected jsExtractElements to contain querySelectorAll")
	}
}

func TestCDPOpenCheck(t *testing.T) {
	isOpen := mgr.isCDPOpen("http://127.0.0.1:9222")
	t.Logf("CDP port 9222 open: %v", isOpen)
}

func TestBrowserNavigation(t *testing.T) {
	// Use headless for CI/unit test
	os.Setenv("BROWSER_HEADLESS", "true")
	defer os.Unsetenv("BROWSER_HEADLESS")

	page, err := mgr.GetPage()
	if err != nil {
		t.Fatalf("Failed to launch browser: %v", err)
	}
	defer mgr.Close()

	if err := page.Navigate("https://example.com"); err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}
	_ = page.WaitLoad()

	summary := getPageSummary(page)
	if !strings.Contains(summary, "Example Domain") {
		t.Fatalf("expected summary to contain 'Example Domain', got: %s", summary)
	}
	t.Logf("Navigation success, summary verified!")
}

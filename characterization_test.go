package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// characterizationHTML is a standalone HTML document served via data: URL (no network needed).
const characterizationHTML = `<!DOCTYPE html>
<html>
<head><title>Characterization Test Page</title></head>
<body>
  <h1>Page Heading</h1>

  <!-- Strategy 1: Direct CSS selector -->
  <button id="direct-btn" type="button">Direct Button</button>

  <!-- Strategy 2: Button/Link matching text (including Playwright :has-text) -->
  <a href="#test" role="link">Click Here Link</a>
  <button role="button" type="button">Action Button</button>

  <!-- Strategy 3: aria-label, placeholder, or name -->
  <input type="text" id="user-input" aria-label="Username Field" value="initial user" />
  <input type="password" id="pass-input" placeholder="Password Placeholder" value="secret" />
  <input type="text" id="search-input" name="search_query" value="initial query" />

  <!-- Strategy 4: Any element with text -->
  <span class="custom-span">Deep Custom Text</span>

  <!-- Textarea for type overwrite testing -->
  <textarea id="desc-box">Initial Description</textarea>
</body>
</html>`

func createCharacterizationPage(t *testing.T) (pageClose func()) {
	t.Helper()
	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(characterizationHTML))

	tabID, _, err := defaultRouter.NewTab(context.Background(), dataURL, "")
	if err != nil {
		t.Fatalf("failed to open characterization test page: %v", err)
	}

	return func() {
		_, _ = defaultRouter.CloseTab(context.Background(), tabID)
	}
}

// 1. Characterization: findElement with 4 selector strategies
func TestCharacterization_FindElement(t *testing.T) {
	cleanup := createCharacterizationPage(t)
	defer cleanup()

	ref, err := defaultRouter.GetTab(context.Background(), "")
	if err != nil {
		t.Fatalf("failed to get active tab ref: %v", err)
	}
	page, err := defaultRodDriver.getPage(ref.handle)
	if err != nil {
		t.Fatalf("failed to get rod page: %v", err)
	}

	// Strategy 1: Direct CSS selector
	el, err := findElement(page, "#direct-btn")
	if err != nil {
		t.Fatalf("Strategy 1 (CSS #direct-btn) failed: %v", err)
	}
	txt, _ := el.Text()
	if txt != "Direct Button" {
		t.Errorf("Strategy 1: expected 'Direct Button', got %q", txt)
	}

	// Strategy 2a: Button/Link text match
	el, err = findElement(page, "Click Here Link")
	if err != nil {
		t.Fatalf("Strategy 2a (link text) failed: %v", err)
	}
	txt, _ = el.Text()
	if txt != "Click Here Link" {
		t.Errorf("Strategy 2a: expected 'Click Here Link', got %q", txt)
	}

	// Strategy 2b: Button with Playwright :has-text("...")
	el, err = findElement(page, `button:has-text("Action Button")`)
	if err != nil {
		t.Fatalf("Strategy 2b (:has-text) failed: %v", err)
	}
	txt, _ = el.Text()
	if txt != "Action Button" {
		t.Errorf("Strategy 2b: expected 'Action Button', got %q", txt)
	}

	// Strategy 3a: aria-label match
	el, err = findElement(page, "Username Field")
	if err != nil {
		t.Fatalf("Strategy 3a (aria-label) failed: %v", err)
	}
	id, _ := el.Attribute("id")
	if id == nil || *id != "user-input" {
		t.Errorf("Strategy 3a: expected id 'user-input', got %v", id)
	}

	// Strategy 3b: placeholder match
	el, err = findElement(page, "Password Placeholder")
	if err != nil {
		t.Fatalf("Strategy 3b (placeholder) failed: %v", err)
	}
	id, _ = el.Attribute("id")
	if id == nil || *id != "pass-input" {
		t.Errorf("Strategy 3b: expected id 'pass-input', got %v", id)
	}

	// Strategy 3c: input name match
	el, err = findElement(page, "search_query")
	if err != nil {
		t.Fatalf("Strategy 3c (name) failed: %v", err)
	}
	id, _ = el.Attribute("id")
	if id == nil || *id != "search-input" {
		t.Errorf("Strategy 3c: expected id 'search-input', got %v", id)
	}

	// Strategy 4: Any element with text
	el, err = findElement(page, "Deep Custom Text")
	if err != nil {
		t.Fatalf("Strategy 4 (general text) failed: %v", err)
	}
	txt, _ = el.Text()
	if !strings.Contains(txt, "Deep Custom Text") {
		t.Errorf("Strategy 4: expected text containing 'Deep Custom Text', got %q", txt)
	}

	// Negative test: element not found
	_, err = findElement(page, "NonExistentElement12345")
	if err == nil {
		t.Errorf("expected error for non-existent element, got nil")
	}
}

// 2. Characterization: evaluate in function form vs raw expression
func TestCharacterization_Evaluate(t *testing.T) {
	cleanup := createCharacterizationPage(t)
	defer cleanup()

	ref, err := defaultRouter.GetTab(context.Background(), "")
	if err != nil {
		t.Fatalf("failed to get active tab ref: %v", err)
	}
	page, err := defaultRodDriver.getPage(ref.handle)
	if err != nil {
		t.Fatalf("failed to get rod page: %v", err)
	}

	// Function form: () => document.title works in rod
	res, err := page.Eval("() => document.title")
	if err != nil {
		t.Fatalf("Eval function form failed: %v", err)
	}
	if res.Value.String() != "Characterization Test Page" {
		t.Errorf("Eval: expected 'Characterization Test Page', got %q", res.Value.String())
	}

	// Raw expression in rod: fails with TypeError because rod wraps as (document.title).apply(...)
	_, err = page.Eval("document.title")
	if err == nil {
		t.Log("Note: raw expression 'document.title' unexpectedly succeeded in rod")
	} else {
		// Documented rod behavior: raw expression fails
		t.Logf("Confirmed rod behavior: raw expression fails with error: %v", err)
	}
}

// 3. Characterization: type overwrites existing content
func TestCharacterization_TypeOverwrite(t *testing.T) {
	cleanup := createCharacterizationPage(t)
	defer cleanup()

	ref, err := defaultRouter.GetTab(context.Background(), "")
	if err != nil {
		t.Fatalf("failed to get active tab ref: %v", err)
	}
	page, err := defaultRodDriver.getPage(ref.handle)
	if err != nil {
		t.Fatalf("failed to get rod page: %v", err)
	}

	// Find the input element which already has value "initial user"
	el, err := findElement(page, "#user-input")
	if err != nil {
		t.Fatalf("failed to find #user-input: %v", err)
	}

	// Replicate the exact type logic from browser_type tool:
	// SelectAllText() + Input("") + Input(text)
	if err := el.SelectAllText(); err == nil {
		_ = el.Input("")
	}
	const replacementText = "brand new user text"
	if err := el.Input(replacementText); err != nil {
		t.Fatalf("failed to input text: %v", err)
	}

	// Verify the value was overwritten, NOT concatenated
	val, err := el.Attribute("value")
	if err != nil || val == nil {
		// Or read via JS property value
		res, _ := page.Eval("() => document.getElementById('user-input').value")
		valStr := res.Value.String()
		if valStr != replacementText {
			t.Errorf("expected value %q, got %q", replacementText, valStr)
		}
	} else if *val != replacementText {
		// Read DOM property value as attribute might not reflect live value
		res, _ := page.Eval("() => document.getElementById('user-input').value")
		valStr := res.Value.String()
		if valStr != replacementText {
			t.Errorf("expected property value %q, got %q", replacementText, valStr)
		}
	}
}

// 4. Characterization: snapshot output format
func TestCharacterization_SnapshotFormat(t *testing.T) {
	cleanup := createCharacterizationPage(t)
	defer cleanup()

	tabID, summary, err := defaultRouter.Snapshot(context.Background(), "")
	if err != nil {
		t.Fatalf("failed to get snapshot: %v", err)
	}

	summaryStr := formatPageSummary(tabID, summary)

	// Verify required sections in summary
	expectedSubstrings := []string{
		fmt.Sprintf("**Tab ID**: `%s`", tabID),
		"**Page Title**: Characterization Test Page",
		"**Interactive Elements**:\n```",
		"- [button] \"Direct Button\" (id=\"direct-btn\" type=\"button\")",
		"- [link] \"Click Here Link\"",
		"- [button] \"Action Button\" (type=\"button\")",
		"- [input] \"initial user\" (id=\"user-input\" type=\"text\")",
		"- [input] \"secret\" (id=\"pass-input\" type=\"password\")",
		"- [input] \"initial query\" (id=\"search-input\" name=\"search_query\" type=\"text\")",
		"- [textarea] \"Initial Description\" (id=\"desc-box\" type=\"textarea\")",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(summaryStr, sub) {
			t.Errorf("summary missing expected substring: %q\nFull summary:\n%s", sub, summaryStr)
		}
	}
}

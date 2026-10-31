package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	content := `
# Comment line
TEST_DOTENV_KEY1=value1
TEST_DOTENV_KEY2="quoted value"
TEST_DOTENV_KEY3='single quoted'
export TEST_DOTENV_KEY4=exported_value
INVALID_LINE_NO_EQUALS
`
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test .env: %v", err)
	}

	// Pre-set one variable to verify it's NOT overwritten
	_ = os.Setenv("TEST_DOTENV_KEY1", "pre_existing")
	defer os.Unsetenv("TEST_DOTENV_KEY1")
	defer os.Unsetenv("TEST_DOTENV_KEY2")
	defer os.Unsetenv("TEST_DOTENV_KEY3")
	defer os.Unsetenv("TEST_DOTENV_KEY4")

	loadDotEnv(envPath)

	if val := os.Getenv("TEST_DOTENV_KEY1"); val != "pre_existing" {
		t.Errorf("expected pre-existing value 'pre_existing', got %q", val)
	}
	if val := os.Getenv("TEST_DOTENV_KEY2"); val != "quoted value" {
		t.Errorf("expected 'quoted value', got %q", val)
	}
	if val := os.Getenv("TEST_DOTENV_KEY3"); val != "single quoted" {
		t.Errorf("expected 'single quoted', got %q", val)
	}
	if val := os.Getenv("TEST_DOTENV_KEY4"); val != "exported_value" {
		t.Errorf("expected 'exported_value', got %q", val)
	}
}

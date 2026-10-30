package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetOrCreateToken(t *testing.T) {
	// Use temporary directory for HOME
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	token, err := GetOrCreateToken()
	if err != nil {
		t.Fatalf("GetOrCreateToken failed: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("expected 64 hex chars, got %d (%s)", len(token), token)
	}

	// Verify file permissions
	tokenPath := filepath.Join(tmpDir, ".browseruse", "token")
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("failed to stat token file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected token perm 0600, got %o", perm)
	}

	// Verify getting again returns the same token
	token2, err := GetOrCreateToken()
	if err != nil {
		t.Fatalf("second GetOrCreateToken failed: %v", err)
	}
	if token != token2 {
		t.Errorf("expected same token, got %s vs %s", token, token2)
	}

	// ValidateToken
	if !ValidateToken(token) {
		t.Errorf("ValidateToken failed for correct token")
	}
	if ValidateToken("wrong-token") {
		t.Errorf("ValidateToken succeeded for wrong token")
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:3025", true},
		{"127.0.0.1", true},
		{"[::1]:3025", true},
		{"::1", true},
		{"192.168.1.10:3025", false},
		{"10.0.0.1:80", false},
		{"8.8.8.8:53", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		got := IsLoopbackAddr(tt.addr)
		if got != tt.want {
			t.Errorf("IsLoopbackAddr(%q) = %v; want %v", tt.addr, got, tt.want)
		}
	}
}

func TestIsOriginAllowed(t *testing.T) {
	// Empty origin is always allowed (for CLI tools)
	if !IsOriginAllowed("") {
		t.Errorf("expected empty origin to be allowed")
	}

	t.Setenv("BROWSERUSE_ALLOWED_ORIGINS", "")
	if IsOriginAllowed("https://evil.com") {
		t.Errorf("expected evil.com to be disallowed when env is empty")
	}

	t.Setenv("BROWSERUSE_ALLOWED_ORIGINS", "https://app.example.com,http://localhost:5173")
	if !IsOriginAllowed("https://app.example.com") {
		t.Errorf("expected app.example.com to be allowed")
	}
	if !IsOriginAllowed("http://localhost:5173") {
		t.Errorf("expected localhost:5173 to be allowed")
	}
	if IsOriginAllowed("https://evil.com") {
		t.Errorf("expected evil.com to be disallowed")
	}
}

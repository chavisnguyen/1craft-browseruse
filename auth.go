package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const tokenFileName = "token"

func getTokenFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home dir: %w", err)
	}
	return filepath.Join(home, ".browseruse", tokenFileName), nil
}

// GetOrCreateToken returns the existing auth token from ~/.browseruse/token or generates a new one.
// If BROWSERUSE_TOKEN environment variable is set, it takes highest precedence.
func GetOrCreateToken() (string, error) {
	if envTok := strings.TrimSpace(os.Getenv("BROWSERUSE_TOKEN")); envTok != "" {
		if len(envTok) == 64 {
			return envTok, nil
		}
		return "", fmt.Errorf("BROWSERUSE_TOKEN must be exactly 64 hex characters (got %d)", len(envTok))
	}

	tokenPath, err := getTokenFilePath()
	if err != nil {
		return "", err
	}

	// Try reading existing token
	if data, err := os.ReadFile(tokenPath); err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) == 64 { // 32 bytes hex encoded
			return token, nil
		}
	}

	// Create directory with 0700
	dir := filepath.Dir(tokenPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	_ = os.Chmod(dir, 0700)

	// Generate 32 cryptographically secure random bytes
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(b)

	// Write token with 0600
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("failed to write token to %s: %w", tokenPath, err)
	}
	_ = os.Chmod(tokenPath, 0600)

	return token, nil
}

// ValidateToken validates a provided token against the stored token using constant-time comparison.
func ValidateToken(provided string) bool {
	token, err := GetOrCreateToken()
	if err != nil || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(strings.TrimSpace(provided))) == 1
}

// IsLoopbackAddr checks if a remote address (e.g. "127.0.0.1:12345" or "[::1]:12345") is loopback.
func IsLoopbackAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// IsOriginAllowed checks whether the HTTP Origin header is permitted.
// If origin is empty, it returns true (cli tools like Claude Code or AGY do not send Origin).
// Otherwise, origin must be in BROWSERUSE_ALLOWED_ORIGINS (comma-separated).
func IsOriginAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	allowedEnv := os.Getenv("BROWSERUSE_ALLOWED_ORIGINS")
	if allowedEnv == "" {
		return false
	}
	parts := strings.Split(allowedEnv, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == origin {
			return true
		}
	}
	return false
}

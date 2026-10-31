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
	return filepath.Join(home, ".1craft-browseruse", tokenFileName), nil
}

func getLegacyTokenFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home dir: %w", err)
	}
	return filepath.Join(home, ".browseruse", tokenFileName), nil
}

// GetOrCreateToken returns the existing auth token from ~/.1craft-browseruse/token (or ~/.browseruse/token).
// If BROWSERUSE_TOKEN or ONECRAFT_BROWSERUSE_TOKEN environment variable is set, it takes highest precedence.
func GetOrCreateToken() (string, error) {
	envTok := strings.TrimSpace(os.Getenv("ONECRAFT_BROWSERUSE_TOKEN"))
	if envTok == "" {
		envTok = strings.TrimSpace(os.Getenv("BROWSERUSE_TOKEN"))
	}
	if envTok != "" {
		if len(envTok) == 64 {
			return envTok, nil
		}
		return "", fmt.Errorf("auth token env must be exactly 64 hex characters (got %d)", len(envTok))
	}

	tokenPath, err := getTokenFilePath()
	if err != nil {
		return "", err
	}
	legacyPath, _ := getLegacyTokenFilePath()

	// 1. Try reading primary token (~/.1craft-browseruse/token)
	if data, err := os.ReadFile(tokenPath); err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) == 64 {
			return token, nil
		}
	}

	// 2. Try reading legacy token (~/.browseruse/token) and migrate
	if legacyPath != "" {
		if data, err := os.ReadFile(legacyPath); err == nil {
			token := strings.TrimSpace(string(data))
			if len(token) == 64 {
				// Migrate to primary directory
				dir := filepath.Dir(tokenPath)
				_ = os.MkdirAll(dir, 0700)
				_ = os.WriteFile(tokenPath, []byte(token+"\n"), 0600)
				return token, nil
			}
		}
	}

	// 3. Generate new 32 cryptographically secure random bytes
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(b)

	// Write primary token with 0600 in 0700 dir
	dir := filepath.Dir(tokenPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	_ = os.Chmod(dir, 0700)
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("failed to write token to %s: %w", tokenPath, err)
	}
	_ = os.Chmod(tokenPath, 0600)

	// Also write legacy token for backward compatibility
	if legacyPath != "" {
		legacyDir := filepath.Dir(legacyPath)
		if err := os.MkdirAll(legacyDir, 0700); err == nil {
			_ = os.Chmod(legacyDir, 0700)
			_ = os.WriteFile(legacyPath, []byte(token+"\n"), 0600)
			_ = os.Chmod(legacyPath, 0600)
		}
	}

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

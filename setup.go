package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// extensionSearchPaths lists where the bundled Chrome extension may live
// (Homebrew pkgshare, next to the binary, a source checkout, or the managed copy).
func extensionSearchPaths() []string {
	var paths []string
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		paths = append(paths,
			filepath.Join(filepath.Dir(exe), "extension"),
			filepath.Join(filepath.Dir(exe), "..", "share", "klik", "extension"),
		)
	}
	paths = append(paths,
		"/opt/homebrew/share/klik/extension",
		"/opt/homebrew/opt/klik/share/klik/extension",
		"/usr/local/share/klik/extension",
		"/home/linuxbrew/.linuxbrew/share/klik/extension",
		"extension",
	)
	return paths
}

func managedExtensionDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".klik", "extension")
}

func hasManifest(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "manifest.json"))
	return err == nil && !fi.IsDir()
}

func findExtensionSource() string {
	for _, p := range extensionSearchPaths() {
		if hasManifest(p) {
			return p
		}
	}
	return ""
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func extensionConfigJSON(token, port string) string {
	return fmt.Sprintf("{\n  \"label\": \"default\",\n  \"token\": %q,\n  \"daemonUrl\": \"ws://127.0.0.1:%s/ext\",\n  \"windowMode\": \"current\"\n}\n", token, port)
}

// runSetup prepares the auth token and the pre-configured Chrome extension copy.
// Registering MCP into AI coding tools is managed centrally by mcps.
func runSetup(port string) error {
	fmt.Printf("🔧 klik setup (v%s)\n\n", Version)

	tok, err := GetOrCreateToken()
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	fmt.Println("✅ Auth token ready")

	src := findExtensionSource()
	if src == "" {
		return fmt.Errorf("extension folder not found (reinstall with 'gear install --force klik' or 'brew reinstall klik')")
	}
	dst := managedExtensionDir()
	if err := copyDir(src, dst); err != nil {
		return fmt.Errorf("copy extension: %w", err)
	}
	cfg := filepath.Join(dst, "default_config.json")
	if err := os.WriteFile(cfg, []byte(extensionConfigJSON(tok, port)), 0600); err != nil {
		return fmt.Errorf("write extension config: %w", err)
	}
	fmt.Printf("✅ Extension prepared (auto-connect, no token to paste): %s\n", dst)

	if pb, err := exec.LookPath("pbcopy"); err == nil {
		c := exec.Command(pb)
		c.Stdin = strings.NewReader(dst)
		if c.Run() == nil {
			fmt.Println("📋 Extension path copied to clipboard")
		}
	}

	client := &http.Client{Timeout: 1500 * time.Millisecond}
	if resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/mcp", port)); err == nil {
		_ = resp.Body.Close()
	} else {
		fmt.Println("\n⚠️  Daemon is not running yet. Start it with: klik service start (or: brew services start klik)")
	}

	fmt.Printf(`
========================================================
🎉 Next steps:
  1. Open chrome://extensions and turn on 'Developer mode'
  2. Click 'Load unpacked' and pick:
       %s
  3. Register endpoint http://localhost:%s/mcp via mcps:
       mcps add klik --url http://localhost:%s/mcp
Problems? Run: klik doctor
========================================================
`, dst, port, port)
	return nil
}

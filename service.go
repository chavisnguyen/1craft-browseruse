package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const launchdLabel = "com.chavisnguyen.klik"

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func systemdUnitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", "klik.service")
}

func klikHomeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".klik")
}

func runServiceCmd(action, port string) error {
	if action == "" {
		action = "status"
	}
	switch action {
	case "install", "start":
		return serviceStart(port)
	case "stop":
		return serviceStop()
	case "restart":
		_ = serviceStop()
		return serviceStart(port)
	case "uninstall":
		return serviceUninstall()
	case "status":
		serviceStatus(port)
		return nil
	default:
		return fmt.Errorf("unknown service action %q (expected: start | stop | restart | status | uninstall)", action)
	}
}

func resolveSelfBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real, nil
	}
	return exe, nil
}

func serviceStart(port string) error {
	exe, err := resolveSelfBinary()
	if err != nil {
		return err
	}
	dir := klikHomeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	logFile := filepath.Join(dir, "daemon.log")

	switch runtime.GOOS {
	case "darwin":
		p := plistPath()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>-sse</string>
    <string>-port</string>
    <string>%s</string>
  </array>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, launchdLabel, exe, port, dir, logFile, logFile)
		if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
			return err
		}
		_ = exec.Command("launchctl", "unload", p).Run()
		if out, err := exec.Command("launchctl", "load", "-w", p).CombinedOutput(); err != nil {
			return fmt.Errorf("launchctl load: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		fmt.Printf("🟢 klik service started on :%s (log: %s)\n", port, logFile)
		return nil

	case "linux":
		u := systemdUnitPath()
		if err := os.MkdirAll(filepath.Dir(u), 0o755); err != nil {
			return err
		}
		unit := fmt.Sprintf(`[Unit]
Description=klik Browser MCP Daemon
After=network.target

[Service]
ExecStart=%s -sse -port %s
WorkingDirectory=%s
Restart=always
RestartSec=2
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, exe, port, dir, logFile, logFile)
		if err := os.WriteFile(u, []byte(unit), 0o644); err != nil {
			return err
		}
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		if out, err := exec.Command("systemctl", "--user", "enable", "--now", "klik.service").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl enable --now: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		fmt.Printf("🟢 klik service started on :%s (log: %s)\n", port, logFile)
		return nil

	default:
		return fmt.Errorf("native service management is only supported on macOS and Linux")
	}
}

func serviceStop() error {
	switch runtime.GOOS {
	case "darwin":
		p := plistPath()
		if _, err := os.Stat(p); err == nil {
			_ = exec.Command("launchctl", "unload", p).Run()
		}
		fmt.Println("🛑 klik service stopped")
		return nil
	case "linux":
		_ = exec.Command("systemctl", "--user", "stop", "klik.service").Run()
		fmt.Println("🛑 klik service stopped")
		return nil
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func serviceUninstall() error {
	_ = serviceStop()
	switch runtime.GOOS {
	case "darwin":
		_ = os.Remove(plistPath())
	case "linux":
		_ = exec.Command("systemctl", "--user", "disable", "klik.service").Run()
		_ = os.Remove(systemdUnitPath())
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	fmt.Println("🗑️  klik service uninstalled")
	return nil
}

func serviceStatus(port string) {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/mcp", port))
	if err == nil {
		_ = resp.Body.Close()
		fmt.Printf("🟢 klik daemon is running on http://localhost:%s/mcp\n", port)
	} else {
		fmt.Printf("⚪ klik daemon is not reachable on :%s (run `klik service start` or `brew services start klik`)\n", port)
	}
}

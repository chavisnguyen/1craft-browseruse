# 1craft BrowserUse

> Let your AI agent (Claude Code, Codex, Cursor, …) use **your real Chrome** — with your logins and tabs.

A tiny (~12MB RAM) local [MCP](https://modelcontextprotocol.io) server written in Go, plus a Chrome extension that bridges it to your browser. No Full Disk Access, no Node/Python.

---

## 🚀 Get started (3 steps, ~2 minutes)

**You need:** macOS/Linux, Google Chrome, [Go 1.22+](https://golang.org) (or use Homebrew below).

### 1. Run setup

```bash
git clone https://github.com/chavisnguyen/1craft-browseruse.git
cd 1craft-browseruse
make setup
```

This builds the binary, creates a token, registers the MCP server in your installed agents (Claude Code, Codex, Cursor, Claude Desktop, OpenCode, Kiro, AGY), and starts the daemon.

<details>
<summary>Prefer Homebrew (no Go needed)?</summary>

```bash
brew tap chavisnguyen/browseruse
brew install browseruse
brew services start browseruse
1craft-browseruse -token      # you'll paste this in the extension
```
Extension folder: `$(brew --prefix)/share/browseruse/extension` — in step 2, open the extension **Options**, paste the token and click **Save & Connect**.
</details>

### 2. Load the extension in Chrome

1. Open `chrome://extensions` and turn on **Developer mode** (top-right).
2. Click **Load unpacked** and choose the `extension/` folder.
   (`make setup` already copied its path to your clipboard: press `Cmd+Shift+G`, `Cmd+V`, `Enter`.)

It connects automatically — no token to paste.

### 3. Try it

Restart your agent, then ask:

> "Open news.ycombinator.com and tell me the top 3 stories."

A terracotta glow around the page means the agent is controlling that tab. Click **Stop** in the top pill to abort.

**Something wrong?** Run `make doctor` — it checks everything and tells you what to fix.

---

## 🧰 Everyday commands

| Command | What it does |
| :--- | :--- |
| `make setup` | One-time install (safe to re-run) |
| `make doctor` | Diagnose problems |
| `make status` | Is the daemon running? |
| `make restart` | Restart the daemon |
| `make stop` | Stop the daemon |
| `make token` | Print the extension token |

Other targets (`make help`): `install`, `install-stdio`, `chrome`, `test`, `clean`.

---

## 🧯 Troubleshooting

| Problem | Fix |
| :--- | :--- |
| Extension shows "Disconnected" / red badge | `make restart`, then `make doctor`. If still red: `make token`, paste in extension **Options** → **Save & Connect**. |
| Yellow bar "started debugging this browser" | Normal — Chrome shows it whenever an extension controls a tab. |
| "multiple extension profiles online" | Several Chrome profiles are connected. Tell the agent which one (e.g. "use the work profile"), or check with `get_browser_status`. |
| "label_in_use" | Two profiles share a label. Rename one in extension **Options**. |
| Agent doesn't see the browser tools | Restart the agent after `make setup`. Endpoint is `http://localhost:3025/mcp`. |

---

## 📚 Advanced

<details>
<summary>Manual MCP setup</summary>

Point any MCP client at `http://localhost:3025/mcp`. Codex example:

```bash
codex mcp add 1craft-browseruse --url http://localhost:3025/mcp
```
Or use stdio mode: `make install-stdio`.
</details>

<details>
<summary>Multiple Chrome profiles</summary>

Load the extension in each profile. Labels are derived from the signed-in account (`alice@company.com` → `company_alice`) and can be changed in **Options**. With one profile online, agents use it automatically; with several, ask e.g. *"open the dashboard in the work profile"* (the agent calls `tabs_create_mcp` with `profile="work"`).
</details>

<details>
<summary>Available tools (14)</summary>

| Tool | Purpose |
| :--- | :--- |
| `tabs_context_mcp`, `tabs_create_mcp`, `tabs_close_mcp` | List / open / close tabs (opened in the "OneCraft Agent" tab group) |
| `navigate` | Go to a URL, back, forward, reload |
| `read_page` | Page text + element refs (`@e1`, `@e2`, …) from the accessibility tree |
| `find` | Find elements by plain-language description |
| `computer` | Click, type, key, scroll, drag, hover, screenshot |
| `form_input` | Fill inputs, select options, toggle checkboxes |
| `file_upload` | Upload files to `<input type="file">` |
| `javascript_tool` | Run JavaScript in the page |
| `read_console_messages`, `read_network_requests` | Debug console and network |
| `browser_batch` | Run several actions in one call |
| `get_browser_status` | Connected profiles and tabs |
</details>

<details>
<summary>Configuration (env vars / .env)</summary>

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `3025` | Daemon + WebSocket port |
| `HOST` | *(loopback)* | Bind host |
| `CHROME_CDP_URL` | `http://127.0.0.1:9222` | Fallback CDP endpoint (no extension) |
| `BROWSER_HEADLESS` | `true` | Headless for the fallback Chrome |
</details>

<details>
<summary>How it works</summary>

```
Agent ──MCP (HTTP/stdio)──▶ Daemon :3025 ──WebSocket──▶ Chrome extension ──▶ your tabs
```
Backend priority: **1.** Extension (recommended) → **2.** Existing Chrome via CDP (needs Full Disk Access on macOS) → **3.** Chrome on port 9222 (`make chrome`) → **4.** Headless Chrome (no cookies).
</details>

<details>
<summary>Security & privacy</summary>

* Binds to loopback only (`127.0.0.1`, `[::1]`).
* Extension connects with a local token (file mode `0600`) and `Origin` is verified.
* Passwords, OTPs and card fields are shown as `[REDACTED]` to the agent.
* Fully local — no telemetry.
</details>

---

## 📄 License

[MIT](LICENSE)

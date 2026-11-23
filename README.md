# klik

> Let your AI agent (Claude Code, Codex, Cursor, …) use **your real Chrome** — with your logins and tabs.

A tiny (~12MB RAM) local [MCP](https://modelcontextprotocol.io) server written in Go, plus a Chrome extension that bridges it to your browser. No Full Disk Access, no Node/Python.

---

## 🚀 Install (macOS / Linux, ~2 minutes)

You only need [Homebrew](https://brew.sh) and Google Chrome.

### 1. Install and set up

```bash
brew tap chavisnguyen/klik https://github.com/chavisnguyen/klik
brew install klik
brew services start klik   # runs the background daemon (auto-starts at login)
klik setup                 # one command: token + extension + register MCP in your agents
```

`klik setup` registers the MCP server in every agent it finds (Claude Code, Codex, Cursor, Claude Desktop, OpenCode, Kiro, AGY) and prepares the Chrome extension so it connects by itself. Safe to re-run.

### 2. Load the extension in Chrome (one-time)

1. Open `chrome://extensions` and turn on **Developer mode** (top-right).
2. Click **Load unpacked** and choose `~/.klik/extension`
   (the path is already in your clipboard: press `Cmd+Shift+G`, `Cmd+V`, `Enter`).

It connects automatically — no token to paste.

### 3. Try it

Restart your AI agent, then ask:

> "Open news.ycombinator.com and tell me the top 3 stories."

A terracotta glow around the page means the agent is controlling that tab. Click **Stop** in the top pill to abort.

**Something wrong?** Run `klik doctor` — it checks everything and tells you what to fix.

---

## 🧰 Commands

Run `klik help` anytime to see this list in your terminal.

| Command | What it does |
| :--- | :--- |
| `klik setup` | One-time setup. Re-run after upgrading or after installing a new agent |
| `klik doctor` | Check everything and suggest fixes |
| `klik help` | Show all commands and flags |
| `klik version` | Print the version |
| `klik -token` | Print the auth token |
| `klik` | Run the daemon in the foreground (HTTP, port 3025; `-port N` to change). Normally you use the brew service instead |
| `klik -stdio` | Stdio MCP server for clients without HTTP support (cannot use the Chrome extension) |

Daemon and package management:

| Command | What it does |
| :--- | :--- |
| `brew services start klik` | Start the background daemon (auto-starts at login) |
| `brew services restart klik` | Restart it |
| `brew services stop klik` | Stop it |
| `brew upgrade klik` | Update, then run `klik setup` and reload the extension at `chrome://extensions` |
| `brew uninstall klik` | Uninstall (also `rm -rf ~/.klik` to remove token and extension copy) |

### Window mode

By default the agent opens tabs in your **current Chrome window**. To keep agent tabs in a separate window instead, open the extension **Options** and set *Window mode* to *Dedicated window*.

---

## 🧯 Troubleshooting

| Problem | Fix |
| :--- | :--- |
| Extension shows "Disconnected" / red badge | `brew services restart klik`, then `klik doctor`. If still red: run `klik setup` again and click reload on the extension at `chrome://extensions`. |
| Yellow bar "started debugging this browser" | Normal — Chrome shows it whenever an extension controls a tab. |
| "multiple extension profiles online" | Several Chrome profiles are connected. Tell the agent which one (e.g. "use the work profile"), or check with `get_browser_status`. |
| "label_in_use" | Two profiles share a label. Rename one in extension **Options**. |
| Agent doesn't see the browser tools | Restart the agent after setup. Endpoint is `http://localhost:3025/mcp`. |
| Claude Desktop can't use your logged-in browser | Claude Desktop only supports stdio, which can't use the extension. Other agents (Claude Code, Codex, Cursor, …) work fully. |
| `brew tap` fails or formula not found | Use the full tap command above, including the repo URL. If you tapped the old `homebrew-klik` repo: `brew untap chavisnguyen/klik` first. |

---

## 📚 Advanced

<details>
<summary>Manual MCP setup</summary>

Point any MCP client at `http://localhost:3025/mcp`. Codex example:

```bash
codex mcp add klik --url http://localhost:3025/mcp
```
Stdio is opt-in (`klik -stdio`, or `make install-stdio`) for clients without HTTP support. It cannot use the Chrome extension, so prefer HTTP.
</details>

<details>
<summary>Multiple Chrome profiles</summary>

Load the extension in each profile. Labels are derived from the signed-in account (`alice@company.com` → `company_alice`) and can be changed in **Options**. With one profile online, agents use it automatically; with several, ask e.g. *"open the dashboard in the work profile"* (the agent calls `tabs_create_mcp` with `profile="work"`).
</details>

<details>
<summary>Available tools (14)</summary>

| Tool | Purpose |
| :--- | :--- |
| `tabs_context_mcp`, `tabs_create_mcp`, `tabs_close_mcp` | List / open / close tabs (opened in the "klik" tab group) |
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
</details>

<details>
<summary>Tools (same names and parameters as Claude in Chrome)</summary>

`tabs_context_mcp`, `tabs_create_mcp`, `tabs_close_mcp`, `navigate`, `read_page`, `find`, `get_page_text`, `form_input`, `computer`, `javascript_tool`, `read_console_messages`, `read_network_requests`, `resize_window`, `file_upload`, `upload_image`, `gif_creator`, `browser_batch`.

Every page tool takes a numeric `tabId` from `tabs_context_mcp` and only acts on tabs in the "klik" tab group. Element references (`ref_N`) come from `read_page` / `find`.
</details>

<details>
<summary>How it works</summary>

```
Agent ──MCP (HTTP)──▶ Daemon :3025 ──WebSocket──▶ Chrome extension ──▶ your tabs
```
The Chrome extension is the only backend.
</details>

<details>
<summary>Build from source (developers)</summary>

Needs [Go 1.22+](https://golang.org).

```bash
git clone https://github.com/chavisnguyen/klik.git
cd klik
make setup      # build + token + register MCP + start daemon; then load ./extension in Chrome
```
Other targets: `make doctor | status | restart | stop | token | install-stdio | chrome | test | clean` (see `make help`).
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

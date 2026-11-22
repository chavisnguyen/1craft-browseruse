#!/bin/bash
set -euo pipefail

# Install browseruse MCP Server into AGY, OpenCode, Claude Code, Kiro, Codex, and Claude Desktop
# Usage:
#   ./install.sh sse    (Recommended for multiple agents: connects to http://localhost:3025/sse)
#   ./install.sh stdio  (Standalone mode: spawns binary on demand)

MODE="${1:-sse}"
TOOL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
BIN_PATH="${BROWSERUSE_BIN:-$TOOL_DIR/bin/browseruse}"
SSE_URL="http://localhost:3025/mcp"
SERVER_NAME="1craft-browseruse"
OLD_NAME="browseruse"

if [ ! -f "$BIN_PATH" ] && [ -z "${BROWSERUSE_BIN:-}" ]; then
    echo "⚙️ Building binary first..."
    (cd "$TOOL_DIR" && go build -o "$BIN_PATH" .)
fi

echo "🔧 Configuring $SERVER_NAME MCP Server in [$MODE] mode..."
if [ "$MODE" = "sse" ]; then
    echo "   SSE URL: $SSE_URL"
else
    echo "   Binary: $BIN_PATH (stdio mode)"
fi
echo ""

# --- 1. AGY: ~/.gemini/config/mcp_config.json ---
AGY_CONFIG="$HOME/.gemini/config/mcp_config.json"
echo "📝 Updating AGY config: $AGY_CONFIG"

python3 - << PYEOF
import json, os

path = os.path.expanduser("~/.gemini/config/mcp_config.json")
if os.path.exists(path):
    with open(path) as f:
        data = json.load(f)
else:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    data = {}

data.setdefault("mcpServers", {})
data["mcpServers"].pop("$OLD_NAME", None)
if "$MODE" == "sse":
    data["mcpServers"]["$SERVER_NAME"] = {
        "disabled": False,
        "serverUrl": "$SSE_URL"
    }
else:
    data["mcpServers"]["$SERVER_NAME"] = {
        "command": "$BIN_PATH"
    }

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("   ✅ AGY config updated")
PYEOF

# --- 2. OpenCode: ~/.config/opencode/opencode.json(c) ---
echo "📝 Updating OpenCode config..."

python3 - << PYEOF
import json, os, re

base = os.path.expanduser("~/.config/opencode")
candidates = [os.path.join(base, "opencode.jsonc"), os.path.join(base, "opencode.json")]
path = next((p for p in candidates if os.path.exists(p)), candidates[1])

def strip_jsonc(text):
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.DOTALL)
    text = re.sub(r"(^|\s)//[^\n]*", r"\1", text)
    text = re.sub(r",(\s*[}\]])", r"\1", text)
    return text

if os.path.exists(path):
    with open(path) as f:
        raw = f.read()
    data = json.loads(strip_jsonc(raw)) if raw.strip() else {}
else:
    os.makedirs(base, exist_ok=True)
    data = {}

data.setdefault("mcp", {}).setdefault("servers", {})
data["mcp"]["servers"].pop("$OLD_NAME", None)
if "$MODE" == "sse":
    data["mcp"]["servers"]["$SERVER_NAME"] = {
        "type": "remote",
        "url": "$SSE_URL"
    }
else:
    data["mcp"]["servers"]["$SERVER_NAME"] = {
        "type": "stdio",
        "command": "$BIN_PATH"
    }

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print(f"   ✅ OpenCode config updated ({os.path.basename(path)})")
PYEOF

# --- 3. Claude Code: ~/.claude.json ---
CC_CONFIG="$HOME/.claude.json"
echo "📝 Updating Claude Code config: $CC_CONFIG"

python3 - << PYEOF
import json, os

path = os.path.expanduser("~/.claude.json")
if os.path.exists(path):
    with open(path) as f:
        data = json.load(f)
else:
    data = {}

data.setdefault("mcpServers", {})
data["mcpServers"].pop("$OLD_NAME", None)
if "$MODE" == "sse":
    data["mcpServers"]["$SERVER_NAME"] = {
        "type": "http",
        "url": "$SSE_URL"
    }
else:
    data["mcpServers"]["$SERVER_NAME"] = {
        "command": "$BIN_PATH"
    }

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("   ✅ Claude Code config updated")
PYEOF

# --- 4. Kiro: ~/.kiro/settings/mcp.json ---
KIRO_CONFIG="$HOME/.kiro/settings/mcp.json"
echo "📝 Updating Kiro config: $KIRO_CONFIG"

python3 - << PYEOF
import json, os

path = os.path.expanduser("~/.kiro/settings/mcp.json")
os.makedirs(os.path.dirname(path), exist_ok=True)

if os.path.exists(path):
    with open(path) as f:
        data = json.load(f)
else:
    data = {}

data.setdefault("mcpServers", {})
data["mcpServers"].pop("$OLD_NAME", None)
if "$MODE" == "sse":
    data["mcpServers"]["$SERVER_NAME"] = {
        "url": "$SSE_URL",
        "disabled": False
    }
else:
    data["mcpServers"]["$SERVER_NAME"] = {
        "command": "$BIN_PATH",
        "disabled": False
    }

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("   ✅ Kiro config updated")
PYEOF

# --- 5. Claude Desktop: ~/Library/Application Support/Claude/claude_desktop_config.json ---
CD_CONFIG="$HOME/Library/Application Support/Claude/claude_desktop_config.json"
if [ -d "$HOME/Library/Application Support/Claude" ]; then
    echo "📝 Updating Claude Desktop config: $CD_CONFIG"
    python3 - << PYEOF
import json, os

path = os.path.expanduser("~/Library/Application Support/Claude/claude_desktop_config.json")
if os.path.exists(path):
    with open(path) as f:
        data = json.load(f)
else:
    data = {}

data.setdefault("mcpServers", {})
data["mcpServers"].pop("$OLD_NAME", None)
data["mcpServers"]["$SERVER_NAME"] = {
    "command": "$BIN_PATH"
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("   ✅ Claude Desktop config updated")
PYEOF
fi

# --- 6. Codex: $CODEX_HOME/config.toml (default ~/.codex/config.toml) ---
CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
CODEX_CONFIG="$CODEX_DIR/config.toml"
echo "📝 Updating Codex config: $CODEX_CONFIG"

CODEX_CONFIG="$CODEX_CONFIG" MODE="$MODE" SSE_URL="$SSE_URL" BIN_PATH="$BIN_PATH" SERVER_NAME="$SERVER_NAME" OLD_NAME="$OLD_NAME" python3 - << 'PYEOF'
import json
import os
import re

path = os.path.expanduser(os.environ["CODEX_CONFIG"])
mode = os.environ["MODE"]
sse_url = os.environ["SSE_URL"]
bin_path = os.environ["BIN_PATH"]
server_name = os.environ["SERVER_NAME"]
old_name = os.environ["OLD_NAME"]

os.makedirs(os.path.dirname(path), exist_ok=True)

if os.path.exists(path):
    with open(path, "r", encoding="utf-8") as f:
        raw = f.read()
else:
    raw = ""

target_names = {server_name, old_name}

def parse_table_header(line):
    stripped = line.strip()
    if stripped.startswith("[[") and "]]" in stripped:
        inner = stripped[2:stripped.index("]]")].strip()
        return inner, True
    if stripped.startswith("[") and "]" in stripped and not stripped.startswith("[["):
        inner = stripped[1:stripped.index("]")].strip()
        return inner, False
    return None, False

def normalize_part(part):
    part = part.strip()
    if (part.startswith('"') and part.endswith('"')) or (part.startswith("'") and part.endswith("'")):
        return part[1:-1]
    return part

def split_toml_path(header):
    parts = []
    cur = []
    quote = None
    escaped = False
    for ch in header:
        if escaped:
            cur.append(ch)
            escaped = False
            continue
        if quote == '"' and ch == "\\":
            cur.append(ch)
            escaped = True
            continue
        if ch in ('"', "'"):
            if quote is None:
                quote = ch
            elif quote == ch:
                quote = None
            cur.append(ch)
        elif ch == "." and quote is None:
            parts.append(normalize_part("".join(cur)))
            cur = []
        else:
            cur.append(ch)
    if cur:
        parts.append(normalize_part("".join(cur)))
    return parts

def is_target_mcp_table(parts):
    return len(parts) >= 2 and parts[0] == "mcp_servers" and parts[1] in target_names

def is_mcp_servers_root(parts):
    return len(parts) == 1 and parts[0] == "mcp_servers"

lines = raw.splitlines(keepends=True)
out_lines = []
skip_section = False
in_mcp_root = False

inline_key_re = re.compile(r'^\s*(?:"([^"]+)"|\'([^\']+)\'|([A-Za-z0-9_-]+))\s*=')

for line in lines:
    header, _ = parse_table_header(line)
    if header is not None:
        parts = split_toml_path(header)
        skip_section = is_target_mcp_table(parts)
        in_mcp_root = is_mcp_servers_root(parts)
        if not skip_section:
            out_lines.append(line)
        continue

    if skip_section:
        continue

    if in_mcp_root:
        m = inline_key_re.match(line)
        if m:
            key = m.group(1) or m.group(2) or m.group(3)
            if key in target_names:
                continue

    out_lines.append(line)

while out_lines and out_lines[-1].strip() == "":
    out_lines.pop()

body = "".join(out_lines)
if body and not body.endswith("\n"):
    body += "\n"
if body:
    body += "\n"

if mode == "sse":
    new_block = f"[mcp_servers.{server_name}]\nurl = {json.dumps(sse_url)}\n"
else:
    new_block = f"[mcp_servers.{server_name}]\ncommand = {json.dumps(bin_path)}\n"

updated = body + new_block

try:
    import tomllib
    tomllib.loads(updated)
except ModuleNotFoundError:
    pass

with open(path, "w", encoding="utf-8") as f:
    f.write(updated)

print("   ✅ Codex config updated")
PYEOF

echo ""
echo "🎉 Done! browseruse configured in [$MODE] mode across your AI tools."
if [ "$MODE" = "sse" ] && [ -z "${BROWSERUSE_BIN:-}" ]; then
    echo "💡 Note: To start the background server, run: make daemon"
fi

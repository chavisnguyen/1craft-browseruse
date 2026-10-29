#!/bin/bash
set -euo pipefail

# Install browseruse Go MCP Binary into AGY, OpenCode, Claude Code, Kiro, and Claude Desktop
TOOL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_PATH="$TOOL_DIR/bin/browseruse"

if [ ! -f "$BIN_PATH" ]; then
    echo "⚙️ Building binary first..."
    go build -o "$BIN_PATH" "$TOOL_DIR/main.go"
fi

echo "🔧 Configuring browseruse (Golang) MCP Server..."
echo "   Binary: $BIN_PATH"
echo ""

# --- 1. AGY: ~/.gemini/config/mcp_config.json ---
AGY_CONFIG="$HOME/.gemini/config/mcp_config.json"
echo "📝 Updating AGY config: $AGY_CONFIG"

python3 << 'PYEOF'
import json, os

path = os.path.expanduser("~/.gemini/config/mcp_config.json")
if os.path.exists(path):
    with open(path) as f:
        data = json.load(f)
else:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    data = {}

data.setdefault("mcpServers", {})
data["mcpServers"]["browseruse"] = {
    "command": "/Users/chavis/Documents/personal/tool/browseruse/bin/browseruse"
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("   ✅ AGY config updated")
PYEOF

# --- 2. OpenCode: ~/.config/opencode/opencode.json(c) ---
echo "📝 Updating OpenCode config..."

python3 << 'PYEOF'
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
data["mcp"]["servers"]["browseruse"] = {
    "type": "stdio",
    "command": "/Users/chavis/Documents/personal/tool/browseruse/bin/browseruse"
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print(f"   ✅ OpenCode config updated ({os.path.basename(path)})")
PYEOF

# --- 3. Claude Code: ~/.claude.json ---
CC_CONFIG="$HOME/.claude.json"
echo "📝 Updating Claude Code config: $CC_CONFIG"

python3 << 'PYEOF'
import json, os

path = os.path.expanduser("~/.claude.json")
if os.path.exists(path):
    with open(path) as f:
        data = json.load(f)
else:
    data = {}

data.setdefault("mcpServers", {})
data["mcpServers"]["browseruse"] = {
    "command": "/Users/chavis/Documents/personal/tool/browseruse/bin/browseruse"
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("   ✅ Claude Code config updated")
PYEOF

# --- 4. Kiro: ~/.kiro/settings/mcp.json ---
KIRO_CONFIG="$HOME/.kiro/settings/mcp.json"
echo "📝 Updating Kiro config: $KIRO_CONFIG"

python3 << 'PYEOF'
import json, os

path = os.path.expanduser("~/.kiro/settings/mcp.json")
os.makedirs(os.path.dirname(path), exist_ok=True)

if os.path.exists(path):
    with open(path) as f:
        data = json.load(f)
else:
    data = {}

data.setdefault("mcpServers", {})
data["mcpServers"]["browseruse"] = {
    "command": "/Users/chavis/Documents/personal/tool/browseruse/bin/browseruse",
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
    python3 << 'PYEOF'
import json, os

path = os.path.expanduser("~/Library/Application Support/Claude/claude_desktop_config.json")
if os.path.exists(path):
    with open(path) as f:
        data = json.load(f)
else:
    data = {}

data.setdefault("mcpServers", {})
data["mcpServers"]["browseruse"] = {
    "command": "/Users/chavis/Documents/personal/tool/browseruse/bin/browseruse"
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("   ✅ Claude Desktop config updated")
PYEOF
fi

echo ""
echo "🎉 Done! browseruse (Golang binary) configured across all AI tools."

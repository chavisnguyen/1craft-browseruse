#!/bin/bash
set -euo pipefail

# Install browseruse MCP Server into AGY, OpenCode, Claude Code, Kiro, and Claude Desktop
# Usage:
#   ./install.sh sse    (Recommended for multiple agents: connects to http://localhost:3025/sse)
#   ./install.sh stdio  (Standalone mode: spawns binary on demand)

MODE="${1:-sse}"
TOOL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_PATH="$TOOL_DIR/bin/browseruse"
SSE_URL="http://localhost:3025/sse"

if [ ! -f "$BIN_PATH" ]; then
    echo "⚙️ Building binary first..."
    go build -o "$BIN_PATH" "$TOOL_DIR/main.go"
fi

echo "🔧 Configuring browseruse MCP Server in [$MODE] mode..."
if [ "$MODE" = "sse" ]; then
    echo "   SSE URL: $SSE_URL (Run 'make daemon' to start server)"
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
if "$MODE" == "sse":
    data["mcpServers"]["browseruse"] = {
        "disabled": False,
        "serverUrl": "$SSE_URL"
    }
else:
    data["mcpServers"]["browseruse"] = {
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
if "$MODE" == "sse":
    data["mcp"]["servers"]["browseruse"] = {
        "type": "remote",
        "url": "$SSE_URL"
    }
else:
    data["mcp"]["servers"]["browseruse"] = {
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
if "$MODE" == "sse":
    data["mcpServers"]["browseruse"] = {
        "type": "http",
        "url": "$SSE_URL"
    }
else:
    data["mcpServers"]["browseruse"] = {
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
if "$MODE" == "sse":
    data["mcpServers"]["browseruse"] = {
        "url": "$SSE_URL",
        "disabled": False
    }
else:
    data["mcpServers"]["browseruse"] = {
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
data["mcpServers"]["browseruse"] = {
    "command": "$BIN_PATH"
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("   ✅ Claude Desktop config updated")
PYEOF
fi

echo ""
echo "🎉 Done! browseruse configured in [$MODE] mode across your AI tools."
if [ "$MODE" = "sse" ]; then
    echo "💡 Note: To start the background server, run: make daemon"
fi

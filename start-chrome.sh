#!/bin/bash
set -euo pipefail

# Start Google Chrome with remote debugging on port 9222
# Usage: ./start-chrome.sh [profile_mode]
#   profile_mode:
#     "main" (default) - uses your daily Chrome profile (requires quitting Chrome first)
#     "isolated"       - uses a dedicated AI profile at ~/.chrome-ai-profile (runs alongside daily Chrome)

MODE="${1:-main}"
CHROME_APP="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
PORT=9222

if [ ! -f "$CHROME_APP" ]; then
    echo "❌ Error: Google Chrome not found at '$CHROME_APP'"
    exit 1
fi

# Check if port 9222 is already listening
if curl -s "http://127.0.0.1:$PORT/json/version" >/dev/null 2>&1; then
    echo "✅ Google Chrome is ALREADY running with remote debugging on port $PORT!"
    echo "   CDP endpoint: http://127.0.0.1:$PORT"
    exit 0
fi

# If Chrome is running normally
if pgrep -f "Google Chrome" >/dev/null 2>&1; then
    if [ "$MODE" = "isolated" ]; then
        echo "ℹ️ Chrome is running. Launching an ISOLATED AI profile on port $PORT..."
        mkdir -p "$HOME/.chrome-ai-profile"
        nohup "$CHROME_APP" --remote-debugging-port=$PORT --user-data-dir="$HOME/.chrome-ai-profile" --no-first-run --no-default-browser-check >/dev/null 2>&1 &
    else
        echo "⚠️ Google Chrome is currently running without remote debugging."
        echo ""
        echo "Option 1 (Recommended to share all your existing logins/cookies):"
        echo "   1. Quit Google Chrome (Cmd + Q)"
        echo "   2. Run this script again: ./start-chrome.sh"
        echo ""
        echo "Option 2 (Run side-by-side without closing your daily Chrome):"
        echo "   Run: ./start-chrome.sh isolated"
        echo ""
        read -p "Would you like to launch the isolated profile now? [y/N]: " choice
        case "$choice" in
            y|Y|yes|Yes )
                mkdir -p "$HOME/.chrome-ai-profile"
                nohup "$CHROME_APP" --remote-debugging-port=$PORT --user-data-dir="$HOME/.chrome-ai-profile" --no-first-run --no-default-browser-check >/dev/null 2>&1 &
                ;;
            * )
                echo "Aborted. Please Cmd+Q Chrome and re-run './start-chrome.sh'."
                exit 0
                ;;
        esac
    fi
else
    echo "🚀 Starting Google Chrome with your main profile on port $PORT..."
    nohup "$CHROME_APP" --remote-debugging-port=$PORT --no-first-run --no-default-browser-check >/dev/null 2>&1 &
fi

# Wait for port to become active
echo "⏳ Waiting for Chrome debugging port $PORT to be ready..."
for i in {1..15}; do
    if curl -s "http://127.0.0.1:$PORT/json/version" >/dev/null 2>&1; then
        echo "🎉 Chrome is ready on port $PORT!"
        echo "   CDP endpoint: http://127.0.0.1:$PORT"
        exit 0
    fi
    sleep 0.5
done

echo "⚠️ Timed out waiting for port $PORT. Please verify Chrome started."

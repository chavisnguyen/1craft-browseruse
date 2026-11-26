# ========================================================
# klik (Control your browser with your AI)
# ========================================================

BIN_DIR := bin
BIN_NAME := klik
BIN_PATH := $(BIN_DIR)/$(BIN_NAME)
PID_FILE := daemon.pid
LOG_FILE := daemon.log
PORT ?= 3025

.PHONY: all build install install-stdio daemon stop restart status test token clean help setup quickstart doctor

all: build

## setup: Tự động hoá 1-click toàn bộ cấu hình, token và chạy daemon
setup: build
	@TOKEN=$$($(BIN_PATH) -token); \
	printf '{\n  "label": "default",\n  "token": "'$$TOKEN'",\n  "daemonUrl": "ws://127.0.0.1:$(PORT)/ext",\n  "windowMode": "current"\n}\n' > extension/default_config.json
	@chmod 600 extension/default_config.json
	@echo "🔑 Đã tự động nhúng token vào extension/default_config.json"
	@$(MAKE) restart
	@EXT_PATH="$$(pwd)/extension"; \
	if command -v pbcopy >/dev/null 2>&1; then \
		printf "$$EXT_PATH" | pbcopy; \
		echo "📋 Đã copy đường dẫn extension vào clipboard: $$EXT_PATH"; \
	fi
	@echo ""
	@echo "========================================================"
	@echo "🎉 SETUP HOÀN TẤT TRONG 1 CLICK!"
	@echo "========================================================"
	@echo "1. Mở Chrome gõ: chrome://extensions"
	@echo "2. Bật 'Developer mode' (góc phải trên)"
	@echo "3. Bấm 'Load unpacked' -> Nhấn Cmd+Shift+G rồi Cmd+V (Enter) để chọn thư mục extension"
	@echo "👉 Extension sẽ TỰ ĐỘNG KẾT NỐI NGAY LẬP TỨC (không cần dán token hay chỉnh Options)!"
	@echo "========================================================"

quickstart: setup

## build: Biên dịch file thực thi Golang nhị phân
build:
	@echo "🔨 Building $(BIN_PATH)..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_PATH) .
	@echo "✅ Build success: $(BIN_PATH)"

## token: Hiển thị auth token dùng cho Chrome extension
token: build
	@$(BIN_PATH) -token

## daemon: Chạy SSE Server dưới nền (dành cho nhiều Agent dùng đồng thời)
daemon: build
	@if [ -f $(PID_FILE) ] && kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		echo "⚠️ Daemon already running (PID: $$(cat $(PID_FILE))) on port $(PORT)"; \
	else \
		echo "🚀 Starting HTTP/SSE Daemon on http://localhost:$(PORT)/mcp ..."; \
		nohup $(BIN_PATH) -sse -port $(PORT) > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE); \
		sleep 1; \
		if kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
			echo "✅ Daemon started successfully (PID: $$(cat $(PID_FILE)))"; \
			echo "   Endpoint: http://localhost:$(PORT)/mcp"; \
		else \
			echo "❌ Failed to start daemon. Check $(LOG_FILE)"; \
			cat $(LOG_FILE); \
		fi \
	fi

## stop: Dừng tiến trình SSE Server daemon
stop:
	@if [ -f $(PID_FILE) ]; then \
		echo "🛑 Stopping daemon (PID: $$(cat $(PID_FILE)))..."; \
		kill $$(cat $(PID_FILE)) 2>/dev/null || true; \
		rm -f $(PID_FILE); \
		echo "✅ Daemon stopped."; \
	else \
		pkill -f "$(BIN_PATH) -sse" 2>/dev/null || true; \
		echo "ℹ️ No PID file found, checked for running processes."; \
	fi

## restart: Khởi động lại daemon
restart: stop daemon

## status: Kiểm tra trạng thái Daemon và Chrome
status:
	@echo "--- MCP Server Status ---"
	@if [ -f $(PID_FILE) ] && kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		echo "🟢 Daemon is RUNNING (PID: $$(cat $(PID_FILE))) on http://localhost:$(PORT)/mcp"; \
	else \
		echo "🔴 Daemon is NOT running (Run 'make daemon' to start)"; \
	fi
	@echo ""

## doctor: Chẩn đoán sức khỏe hệ thống (token, daemon, extension folder)
doctor: build
	@$(BIN_PATH) doctor

## test: Chạy kiểm thử tự động
test: build
	@echo "🧪 Running integration test..."
	@go test -v ./...
	@node --test extension/test

## clean: Dọn dẹp binary, log và file tạm
clean: stop
	@echo "🧹 Cleaning up..."
	@rm -rf $(BIN_DIR) $(LOG_FILE) $(PID_FILE) /tmp/duckduckgo_*.png /tmp/screenshot*.png /tmp/test_*.png
	@echo "✅ Clean done."

## help: Hiển thị hướng dẫn
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  make setup         - ⭐ Cài đặt 1 lần: build, token, đăng ký MCP, chạy daemon"
	@echo "  make doctor        - Chẩn đoán lỗi và gợi ý cách sửa"
	@echo "  make daemon        - Bật SSE Server ngầm (Port 3025) cho nhiều Agent"
	@echo "  make stop          - Dừng SSE Server"
	@echo "  make restart       - Khởi động lại SSE Server"
	@echo "  make status        - Kiểm tra trạng thái Daemon và Chrome"
	@echo "  make token         - Hiển thị auth token dùng cho Chrome extension"
	@echo "  make install       - Cài đặt MCP chế độ SSE vào các Agent (mặc định)"
	@echo "  make install-stdio - Cài đặt MCP chế độ stdio vào các Agent"
	@echo "  make test          - Chạy test kiểm thử"
	@echo "  make clean         - Dọn dẹp binary và log"

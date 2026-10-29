# ========================================================
# browseruse MCP Server (Golang Multi-Agent Engine)
# ========================================================

BIN_DIR := bin
BIN_NAME := browseruse
BIN_PATH := $(BIN_DIR)/$(BIN_NAME)
PID_FILE := daemon.pid
LOG_FILE := daemon.log
PORT ?= 3025

.PHONY: all build install install-stdio daemon stop restart status chrome test clean help

all: build

## build: Biên dịch file thực thi Golang nhị phân
build:
	@echo "🔨 Building $(BIN_PATH)..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_PATH) ./main.go
	@echo "✅ Build success: $(BIN_PATH)"

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
	@echo "--- Chrome Remote Debugging (Port 9222) ---"
	@if curl -s http://127.0.0.1:9222/json/version >/dev/null 2>&1; then \
		echo "🟢 Google Chrome connected on port 9222"; \
	else \
		echo "🔴 Chrome port 9222 not active (Run 'make chrome' to attach your real Chrome)"; \
	fi

## install: Cấu hình MCP chế độ SSE (khuyên dùng cho nhiều Agent)
install: build
	@./install.sh sse

## install-stdio: Cấu hình MCP chế độ stdio (đơn lẻ từng Agent)
install-stdio: build
	@./install.sh stdio

## chrome: Bật Google Chrome với cổng remote debugging 9222 (giữ login & nhẹ RAM)
chrome:
	@./start-chrome.sh

## test: Chạy kiểm thử tự động
test: build
	@echo "🧪 Running integration test..."
	@go test -v ./...

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
	@echo "  make daemon        - Bật SSE Server ngầm (Port 3025) cho nhiều Agent"
	@echo "  make stop          - Dừng SSE Server"
	@echo "  make status        - Kiểm tra trạng thái Daemon và Chrome"
	@echo "  make install       - Cài đặt MCP chế độ SSE vào các Agent (mặc định)"
	@echo "  make install-stdio - Cài đặt MCP chế độ stdio vào các Agent"
	@echo "  make chrome        - Bật Chrome cổng 9222 để dùng chung"
	@echo "  make test          - Chạy test kiểm thử"
	@echo "  make clean         - Dọn dẹp binary và log"

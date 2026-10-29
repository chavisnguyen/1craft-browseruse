# ========================================================
# browseruse MCP Server (Golang)
# ========================================================

BIN_DIR := bin
BIN_NAME := browseruse
BIN_PATH := $(BIN_DIR)/$(BIN_NAME)

.PHONY: all build install chrome clean test run help

all: build

## build: Biên dịch file thực thi Golang nhị phân
build:
	@echo "🔨 Building $(BIN_PATH)..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_PATH) ./main.go
	@echo "✅ Build success: $(BIN_PATH)"

## install: Cấu hình MCP vào AGY, Claude Code, OpenCode, Kiro, Claude Desktop
install: build
	@./install.sh

## chrome: Mở Google Chrome với cổng remote debugging 9222 (giữ login & nhẹ RAM)
chrome:
	@./start-chrome.sh

## test: Chạy kiểm thử tự động
test: build
	@echo "🧪 Running integration test..."
	@go test -v ./...

## clean: Dọn dẹp binary và file tạm
clean:
	@echo "🧹 Cleaning up..."
	@rm -rf $(BIN_DIR) /tmp/duckduckgo_*.png /tmp/screenshot*.png /tmp/test_*.png
	@echo "✅ Clean done."

## help: Hiển thị hướng dẫn
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  make build    - Biên dịch Go binary (bin/browseruse)"
	@echo "  make install  - Biên dịch và tự động cắm MCP vào tất cả các Agent"
	@echo "  make chrome   - Bật Chrome cổng 9222 để dùng chung"
	@echo "  make test     - Chạy test kiểm thử"
	@echo "  make clean    - Dọn dẹp binary và file ảnh test"

# browseruse MCP Server (Golang Multi-Agent Engine)

MCP Server điều khiển trình duyệt viết bằng **Golang**, hỗ trợ chạy nền **SSE Server (cổng 3025)** hoặc **stdio** cho phép **nhiều Agent dùng song song cùng lúc**.

## 🚀 Tính năng nổi bật

1. **Hỗ trợ đa Agent đồng thời (Multi-Agent Concurrency):**
   * Chạy nền 1 tiến trình Go duy nhất (`make daemon`).
   * Quản lý Tab Pool: Nhiều Agent kết nối vào cùng lúc sẽ tự động dùng các Tab độc lập (`tab-1`, `tab-2`...), không bao giờ tranh chấp hay đè thao tác của nhau.
2. **Siêu nhẹ RAM (~12MB):**
   * Binary Go tiêu thụ chỉ ~12MB RAM khi chạy daemon phục vụ cho tất cả các Agent.
3. **Cắm vào Chrome đang mở (Port 9222):**
   * Toàn bộ các Tab của các Agent đều mở trong cùng một cửa sổ Google Chrome của bạn, dùng chung mọi tài khoản, cookie đã đăng nhập và 0 MB RAM dư thừa.
4. **Không cần API Key:**
   * Dùng trực tiếp trí tuệ của Agent (Claude, Gemini, GPT) để phân tích giao diện và ra lệnh.

---

## 🛠 Lệnh Makefile nhanh

| Lệnh | Ý nghĩa |
| :--- | :--- |
| `make daemon` | Bật SSE Server ngầm tại `http://localhost:3025/sse` |
| `make stop` | Tắt SSE Server ngầm |
| `make restart` | Khởi động lại Server |
| `make status` | Kiểm tra trạng thái Server và kết nối Chrome cổng 9222 |
| `make install` | Cài đặt MCP chế độ SSE (`http://localhost:3025/sse`) vào AGY, Claude Code, OpenCode, Kiro, Claude Desktop |
| `make install-stdio` | Cài đặt MCP chế độ stdio (chạy binary trực tiếp khi cần) |
| `make chrome` | Bật Google Chrome với cổng remote debugging 9222 |
| `make test` | Chạy bộ kiểm thử tự động đa Tab |
| `make clean` | Dọn dẹp binary, file log, pid |

---

## 🌐 Các Tool cung cấp cho Agent

* `browser_new_tab(url)`: Mở một Tab mới độc lập (trả về `tab_id`, ví dụ `tab-1`).
* `browser_list_tabs()`: Liệt kê tất cả các tab đang mở, tab nào đang active.
* `browser_close_tab(tab_id)`: Đóng tab cụ thể khi làm xong.
* `browser_navigate(url, tab_id)`: Mở link trên tab chỉ định (hoặc tab active).
* `browser_snapshot(tab_id)`: Đọc cấu trúc UI, các nút bấm, ô input của tab.
* `browser_click(selector, tab_id)`: Bấm vào nút, link trên tab.
* `browser_type(selector, text, press_enter, tab_id)`: Điền nội dung vào ô input.
* `browser_press_key(key, tab_id)`: Bấm phím bàn phím trên tab.
* `browser_screenshot(filename, tab_id)`: Chụp ảnh màn hình của tab lưu file PNG.
* `browser_evaluate(script, tab_id)`: Thực thi code JavaScript trên tab.
* `get_browser_status()`: Xem trạng thái hệ thống và danh sách tab.
* `browser_close()`: Đóng toàn bộ các tab và tài nguyên.

---

## 💡 Hướng dẫn sử dụng hàng ngày

```bash
# 1. Bật Chrome của bạn (để giữ login)
make chrome

# 2. Bật daemon chạy nền (cho mọi Agent cắm vào)
make daemon
```
Sau đó bạn có thể mở đồng thời **Claude Code**, **AGY**, hay **Cursor** và ra lệnh duyệt web mà không sợ bị đè thao tác!

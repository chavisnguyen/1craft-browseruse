# browseruse MCP Server (Golang Engine)

MCP Server điều khiển trình duyệt trực tiếp viết bằng **Golang** dành cho tất cả AI Agent (**AGY, Claude Code, Cursor, Kiro, OpenCode, Claude Desktop**).

## ⚡ Ưu điểm vượt trội của bản Golang

1. **Khởi động tức thì (< 0.01 giây):** File nhị phân tĩnh (Static Binary), không tốn thời gian nạp Python runtime.
2. **Siêu nhẹ RAM (~10MB):** Binary Go chỉ tiêu thụ khoảng 10MB RAM, nhẹ hơn nhiều so với Python.
3. **Độc lập, không cần cài Python/uv:** Biên dịch thành 1 file duy nhất `bin/browseruse`.
4. **Hỗ trợ Makefile chuẩn chỉnh:** Quản lý toàn bộ vòng đời ứng dụng chỉ bằng các lệnh `make`.
5. **Không cần API Key:** Dùng trực tiếp bộ não của Agent bạn đang chat, MCP chỉ là "tay chân" thực thi.

---

## 🚀 Thao tác nhanh với Makefile

| Lệnh | Ý nghĩa |
| :--- | :--- |
| `make build` | Biên dịch file nhị phân Go vào `bin/browseruse` |
| `make install` | Biên dịch và tự động cắm MCP vào **AGY, Claude Code, OpenCode, Kiro, Claude Desktop** |
| `make chrome` | Bật Google Chrome với cổng debug 9222 (dùng chung cookie/login, 0 MB RAM thừa) |
| `make test` | Chạy test tự động |
| `make clean` | Dọn dẹp thư mục `bin/` và các file tạm |

---

## 🛠 Danh sách Tool MCP cung cấp cho Agent

* `browser_navigate(url)`: Mở trang web và tự động đọc cấu trúc UI (danh sách nút bấm, ô input, link).
* `browser_snapshot()`: Đọc lại toàn bộ trạng thái màn hình hiện tại.
* `browser_click(selector)`: Bấm vào nút, link (hỗ trợ text như `Đăng nhập`, CSS selector, aria-label).
* `browser_type(selector, text, press_enter)`: Điền thông tin vào ô input và nhấn Enter nếu cần.
* `browser_press_key(key)`: Bấm phím (Enter, Tab, Escape, Down...).
* `browser_screenshot(filename)`: Chụp ảnh màn hình lưu file PNG.
* `browser_evaluate(script)`: Chạy code JavaScript trên trang web.
* `get_browser_status()`: Kiểm tra trạng thái cổng 9222 và Chrome.
* `browser_close()`: Đóng tab/trình duyệt giải phóng tài nguyên.

---

## 🌐 Cách dùng hàng ngày

1. **Mở Chrome chung:**
   ```bash
   make chrome
   ```
2. **Chat với bất kỳ AI nào (Claude Code, AGY, Cursor...):**
   > *"Dùng browseruse vào localhost:5174, đăng nhập bằng tài khoản test rồi kiểm tra bảng học viên."*

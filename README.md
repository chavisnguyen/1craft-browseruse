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
| `make daemon` | Bật SSE Server ngầm tại `http://localhost:3025/mcp` |
| `make stop` | Tắt SSE Server ngầm |
| `make restart` | Khởi động lại Server |
| `make status` | Kiểm tra trạng thái Server và kết nối Chrome cổng 9222 |
| `make token` | Lấy auth token để cấu hình kết nối Chrome extension |
| `make install` | Cài đặt MCP chế độ SSE (`http://localhost:3025/mcp`) vào AGY, Claude Code, OpenCode, Kiro, Claude Desktop |
| `make install-stdio` | Cài đặt MCP chế độ stdio (chạy binary trực tiếp khi cần) |
| `make chrome` | Bật Chrome với cổng HTTP 9222 (chỉ cần khi không dùng `chrome://inspect`) |
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
make daemon     # bật daemon chạy nền (một lần, mọi Agent dùng chung)
make status     # kiểm tra: daemon 🟢 và Chrome 🟢
```
Sau đó mở đồng thời **Claude Code**, **AGY**, hay **Cursor** và ra lệnh duyệt web mà không sợ bị đè thao tác. Endpoint MCP: `http://localhost:3025/mcp`.

---

## 🔌 Server lấy Chrome từ đâu?

Mỗi lần có Agent gọi tool, server tìm trình duyệt theo thứ tự:

| Thứ tự | Cách | Khi nào | Có login của bạn? | Icon Dock? |
| :--- | :--- | :--- | :--- | :--- |
| 1 | **Chrome hằng ngày** (qua `chrome://inspect`) | Đã bật công tắc, xem mục "Cài đặt lần đầu" | ✅ | Không thêm |
| 2 | **Chrome có cổng HTTP** `http://127.0.0.1:9222` | Chạy `make chrome` hoặc `./start-chrome.sh isolated` | ✅ (main) / ❌ (isolated) | Không thêm / thêm 1 |
| 3 | **Chrome headless dự phòng** | Hai cách trên đều không có | ❌ profile trống | Không có |

Chrome tự launch (cách 3) sẽ tự tắt khi đóng tab cuối, và tự launch lại nếu bị tắt giữa chừng. Chrome ở cách 1 và 2 thì server **không bao giờ tắt** giúp bạn.

### Cài đặt lần đầu (dùng Chrome hằng ngày)

Chrome hiện tại (136+) chỉ cho Agent nối vào profile hằng ngày qua cơ chế remote debugging riêng, không còn endpoint HTTP `/json/version` (trả `404`).

1. Mở `chrome://inspect/#remote-debugging` trong Chrome, **bật công tắc**. Nếu Chrome hiện hộp thoại xin phép khi Agent kết nối, bấm Allow.
2. Cấp quyền đọc profile Chrome cho terminal chạy daemon, vì macOS chặn đọc file `~/Library/Application Support/Google/Chrome/DevToolsActivePort`:
   * System Settings → Privacy & Security → **Full Disk Access** → bật cho app terminal bạn dùng (Terminal, iTerm, cmux...).
   * **Thoát hẳn** terminal (Cmd+Q) rồi mở lại.
   * Kiểm tra: `cat ~/Library/Application\ Support/Google/Chrome/DevToolsActivePort` phải in ra 2 dòng (port và `/devtools/browser/<id>`).
3. Chạy `make restart && make status`, phải thấy cả 🟢 daemon và 🟢 Chrome.
4. Trong Claude Code gõ `/mcp` và Reconnect `browseruse` nếu cần.

Server tự đọc lại `DevToolsActivePort` ở mỗi lần dùng, nên Chrome khởi động lại (id websocket đổi) cũng không cần cấu hình lại.

### Biến môi trường

| Biến | Ý nghĩa |
| :--- | :--- |
| `CHROME_CDP_URL` | Địa chỉ Chrome. Mặc định `http://127.0.0.1:9222`. Nhận cả `ws://127.0.0.1:9222/devtools/browser/<id>` nếu không đọc được `DevToolsActivePort` |
| `BROWSER_HEADLESS` | Chỉ ảnh hưởng Chrome tự launch (cách 3). Mặc định headless. Đặt `false` để thấy cửa sổ (sẽ có thêm icon Dock) |

---

## 🧯 Xử lý sự cố

| Triệu chứng | Nguyên nhân / Cách sửa |
| :--- | :--- |
| `get_browser_status` báo 🔴 "Standalone Chrome" | Server không nối được Chrome hằng ngày. Kiểm tra: công tắc `chrome://inspect/#remote-debugging` còn bật; terminal có Full Disk Access; `make status` xanh. Log: `daemon.log` (dòng `resolveCDP: cannot read DevToolsActivePort`) |
| `operation not permitted` khi đọc `DevToolsActivePort` | macOS chặn. Làm lại bước 2 ở "Cài đặt lần đầu", nhớ Cmd+Q terminal. Lưu ý: tiến trình chạy trong sandbox của Claude Code cũng bị chặn, hãy chạy `make restart` từ terminal của bạn, không từ Agent |
| Hai icon Chrome trong Dock | Từng xảy ra khi server tự launch Chrome không headless. Bản hiện tại mặc định headless; nếu còn sót thì `pkill -f "rod/user-data"` hoặc `make restart` |
| `curl http://127.0.0.1:9222/json/version` trả `404` | Bình thường với Chrome hằng ngày bật qua `chrome://inspect`; server không dùng endpoint này |
| MCP báo `ECONNREFUSED` trong Claude Code | Daemon đã tắt (thoát terminal, tắt máy...). `make daemon` rồi `/mcp` Reconnect |
| Lỗi biên dịch khi phát triển | Chạy `go build .` hoặc `make build` để biên dịch toàn bộ package main (gồm nhiều file Go). |
| Đăng nhập mất khi Chrome bị tắt | Server rơi về Chrome headless dự phòng (profile trống). Mở lại Chrome hằng ngày, các lần gọi sau tự nối lại |

---

## ⚠️ Lưu ý bảo mật

Khi nối vào Chrome hằng ngày, Agent thao tác bằng **chính tài khoản và cookie đang đăng nhập của bạn**. Chỉ giao cho Agent những việc bạn chấp nhận để nó làm dưới danh nghĩa mình. Muốn tách biệt, dùng profile riêng: `./start-chrome.sh isolated` (profile `~/.chrome-ai-profile`, cổng 9222, cần Chrome hằng ngày chưa chiếm cổng đó).

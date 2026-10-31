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

* `list_profiles()`: Liệt kê các profile Chrome Extension đang online (nhãn, email nếu có, số tab quản lý).
* `browser_new_tab(url, profile)`: Mở một Tab mới độc lập (trả về `tab_id`, ví dụ `tab-1`). Hỗ trợ chọn profile extension (ví dụ `work`, `personal`).
* `browser_list_tabs()`: Liệt kê tất cả các tab đang mở, nhãn profile của từng tab, và tab nào đang active.
* `browser_close_tab(tab_id)`: Đóng tab cụ thể khi làm xong.
* `browser_navigate(url, tab_id)`: Mở link trên tab chỉ định (hoặc tab active).
* `browser_snapshot(tab_id)`: Đọc cấu trúc UI, các nút bấm, ô input của tab.
* `browser_click(selector, tab_id)`: Bấm vào nút, link trên tab.
* `browser_type(selector, text, press_enter, tab_id)`: Điền nội dung vào ô input.
* `browser_press_key(key, tab_id)`: Bấm phím bàn phím trên tab.
* `browser_screenshot(filename, tab_id)`: Chụp ảnh màn hình của tab lưu file PNG.
* `browser_evaluate(script, tab_id)`: Thực thi code JavaScript trên tab.
* `get_browser_status()`: Xem trạng thái hệ thống, danh sách profile extension và các tab đang mở.
* `browser_close()`: Đóng toàn bộ các tab và tài nguyên do daemon quản lý.

---

## 💡 Hướng dẫn sử dụng hàng ngày

```bash
make daemon     # bật daemon chạy nền (một lần, mọi Agent dùng chung)
make status     # kiểm tra: daemon 🟢 và Chrome 🟢
```
Sau đó mở đồng thời **Claude Code**, **AGY**, hay **Cursor** và ra lệnh duyệt web mà không sợ bị đè thao tác. Endpoint MCP: `http://localhost:3025/mcp`.

---

## 🔌 Server lấy Chrome từ đâu?

Mỗi lần có Agent gọi tool mở tab, server tìm backend trình duyệt theo thứ tự ưu tiên:

| Thứ tự | Backend | Khi nào kích hoạt | Quyền macOS | Profile & Cookie | Cửa sổ thao tác |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **1 (Khuyên dùng)** | **Chrome Extension Bridge** | Có profile extension online | 🟢 **Không cần Full Disk Access** | ✅ Đa profile đồng thời (`work`, `personal`...), cookie thật | Cửa sổ riêng (Dedicated), không chiếm chuột/focus |
| 2 | **Chrome hằng ngày (CDP)** | Bật `chrome://inspect`, không có extension | 🟡 Cần **Full Disk Access** cho terminal | ✅ Profile chính | Cửa sổ Chrome đang dùng |
| 3 | **Chrome cổng HTTP 9222** | Chạy `make chrome` hoặc `./start-chrome.sh` | 🟢 Không cần | ✅ (main) / ❌ (isolated) | Cửa sổ riêng |
| 4 | **Chrome Standalone dự phòng** | Không có 3 cách trên | 🟢 Không cần | ❌ Profile tạm (mất cookie khi tắt) | Headless ngầm |

---

## 🧩 Cài đặt Chrome Extension (Khuyên dùng)

Cài extension giúp Agent điều khiển Chrome ổn định, **không cần cấp quyền Full Disk Access** và **hỗ trợ nhiều profile cùng lúc**:

1. Lấy mã token xác thực:
   ```bash
   make token
   ```
2. Mở trình duyệt Chrome trên profile bạn muốn dùng (ví dụ profile cá nhân hoặc profile công việc):
   * Truy cập `chrome://extensions`.
   * Bật công tắc **Developer mode** ở góc phải trên.
   * Bấm **Load unpacked** (Tải tiện ích đã giải nén) và chọn thư mục `extension/` trong repo `browseruse`.
3. Cấu hình Extension:
   * Bấm vào biểu tượng extension hoặc chuột phải vào icon tiện ích → **Options** (Tuỳ chọn).
   * **Profile Label**: Đặt tên nhãn phân biệt (ví dụ: `work`, `personal`, `default`).
   * **Auth Token**: Dán mã token lấy được từ lệnh `make token` ở Bước 1.
   * **Chế độ cửa sổ**: Mặc định là `Cửa sổ riêng (Dedicated Window)` giúp Agent chạy tab riêng mà không làm phiền cửa sổ bạn đang lướt web.
   * Bấm **Lưu & Kết nối**. Trạng thái sẽ chuyển sang 🟢 **Đã kết nối (Online)**.
4. *(Tuỳ chọn)* Lặp lại các bước trên trên các profile Chrome khác với nhãn khác (`personal`, `client_a`...) nếu muốn Agent làm việc đa profile song song.

---

## 👥 Chọn Profile Extension khi làm việc

* **Chỉ có 1 profile online**: Agent gọi `browser_new_tab(url)` sẽ tự động chọn profile đó mà không cần chỉ định.
* **Có từ 2 profile online trở lên**:
  * Agent hoặc bạn chỉ định rõ profile khi mở tab:
    ```
    browser_new_tab(url="https://mail.google.com", profile="work")
    ```
  * Nếu không chỉ định `profile`, tool sẽ báo lỗi thân thiện liệt kê tất cả các profile đang online để Agent chọn lại.
* **Xem danh sách profile online**: Gọi tool `list_profiles()` hoặc `get_browser_status()`.

---

## ⚖️ Bảng so sánh các Backend

| Tiêu chí | Chrome Extension Bridge | Chrome hằng ngày (CDP 9222) | Standalone Chrome |
| :--- | :--- | :--- | :--- |
| **Quyền macOS** | 🟢 Không cần Full Disk Access | 🔴 Bắt buộc Full Disk Access | 🟢 Không cần |
| **Đa profile đồng thời** | 🟢 Hỗ trợ song song nhiều profile (`work`, `personal`...) | 🔴 Chỉ 1 profile | 🔴 Không hỗ trợ |
| **Đăng nhập & Cookie** | 🟢 Đầy đủ tài khoản, cookie của từng profile | 🟢 Đầy đủ tài khoản của profile chính | 🔴 Profile rỗng, mất khi tắt |
| **Trải nghiệm dùng** | 🟢 Mở cửa sổ riêng kích thước chuẩn 1280x900, không cướp focus chuột | 🟡 Mở tab trực tiếp trong cửa sổ Chrome bạn đang lướt | ⚪ Chạy ngầm headless |
| **Tính ổn định** | 🟢 Không phụ thuộc format file `DevToolsActivePort` | 🟡 Dễ lệch port khi Chrome tự cập nhật | 🟢 Ổn định nhưng không có cookie |

---

### Cài đặt dự phòng qua CDP (Nếu không dùng Extension)

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
| Extension Options báo "Chưa kết nối" / Badge `!` đỏ | Daemon chưa chạy hoặc token không đúng. Chạy `make daemon`, sau đó chạy `make token` và dán lại token chính xác vào Options của extension rồi bấm Lưu. |
| Thanh vàng *"browseruse started debugging this browser"* | Cảnh báo tiêu chuẩn của Chrome khi extension kết nối điều khiển tab qua `chrome.debugger`. Hoàn toàn bình thường và an toàn. |
| Báo lỗi *"multiple extension profiles online"* | Đang có từ 2 profile extension online cùng lúc. Cần truyền thêm tham số `profile` trong `browser_new_tab(url="...", profile="work")`. |
| Báo lỗi *"label_in_use"* | Hai instance extension cấu hình cùng một nhãn profile. Hãy mở Options và đổi tên nhãn phân biệt (ví dụ `work`, `personal`). |
| `get_browser_status` báo 🔴 "Standalone Chrome" | Không có extension online và server không nối được Chrome hằng ngày qua CDP. Kiểm tra: extension đã nạp và online chưa, hoặc công tắc `chrome://inspect/#remote-debugging` còn bật không và terminal có Full Disk Access chưa. Log: `daemon.log`. |
| `operation not permitted` khi đọc `DevToolsActivePort` | macOS chặn đọc profile Chrome khi dùng backend CDP cũ. Cách tốt nhất: dùng Chrome Extension để không cần Full Disk Access. Nếu vẫn muốn dùng CDP, cấp Full Disk Access cho terminal rồi Cmd+Q mở lại. |
| Hai icon Chrome trong Dock | Từng xảy ra khi server tự launch Chrome không headless. Bản hiện tại mặc định headless; nếu còn sót thì `pkill -f "rod/user-data"` hoặc `make restart` |
| `curl http://127.0.0.1:9222/json/version` trả `404` | Bình thường với Chrome hằng ngày bật qua `chrome://inspect`; server không dùng endpoint này |
| MCP báo `ECONNREFUSED` trong Claude Code / AGY | Daemon đã tắt (thoát terminal, tắt máy...). Chạy `make daemon` rồi Reconnect MCP |
| Lỗi biên dịch khi phát triển | Chạy `go build .` hoặc `make build` để biên dịch toàn bộ package main (gồm nhiều file Go). |
| Đăng nhập mất khi Chrome bị tắt | Server rơi về Chrome headless dự phòng (profile trống). Khởi động lại Chrome/Extension, các lần gọi sau tự nối lại |

---

## ⚠️ Lưu ý bảo mật

Khi nối vào Chrome hằng ngày, Agent thao tác bằng **chính tài khoản và cookie đang đăng nhập của bạn**. Chỉ giao cho Agent những việc bạn chấp nhận để nó làm dưới danh nghĩa mình. Muốn tách biệt, dùng profile riêng: `./start-chrome.sh isolated` (profile `~/.chrome-ai-profile`, cổng 9222, cần Chrome hằng ngày chưa chiếm cổng đó).

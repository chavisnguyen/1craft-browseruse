# PLAN: Chrome Extension bridge cho browseruse

> Tài liệu giao việc cho AI thực thi (AGY). Người đọc không có ngữ cảnh cuộc trao đổi trước, nên mọi quyết định đã chốt đều nằm ở đây.
> Trạng thái: **chưa làm**. Ngày ghi: 2026-10-30. Tech lead: Claude. Người duyệt: chủ repo.

---

## 0. Tóm tắt một đoạn

`browseruse` là MCP server viết bằng Go (`main.go`, cổng 3025, nhiều Agent dùng chung) điều khiển Chrome qua CDP bằng thư viện `rod`. Cách này buộc phải đọc `~/Library/Application Support/Google/Chrome/DevToolsActivePort`, mà macOS chỉ cho đọc khi cấp **Full Disk Access**, và không chọn được profile Chrome. Nhiệm vụ: thêm một **Chrome extension (MV3)** làm backend thứ hai. Mỗi profile Chrome cài một bản, mang một **nhãn profile** (vd `work`, `personal`) và kết nối WebSocket tới daemon. Agent gọi tool kèm `profile` để chọn profile. Extension không cần Full Disk Access. Backend `rod` cũ **giữ nguyên làm đường dự phòng**.

## 1. Ngữ cảnh kỹ thuật bắt buộc phải biết

### 1.1 Repo hiện tại (`/Users/chavis/Documents/personal/tool/browseruse`)

| File | Vai trò |
| :--- | :--- |
| `main.go` (~720 dòng) | Toàn bộ server: `BrowserManager`, `findElement`, `jsExtractElements`, `getPageSummary`, 12 tool MCP, `main()` |
| `main_test.go` | 3 test (cần mạng + Chrome cho test multi-tab) |
| `Makefile` | `build` đang chạy `go build -o bin/browseruse ./main.go` (**chỉ 1 file**) |
| `install.sh` | Cũng build bằng `go build ... main.go` và ghi cấu hình MCP vào AGY/Claude Code/... |
| `start-chrome.sh` | Bật Chrome với cổng 9222 (đường CDP cũ) |
| `README.md` | Hướng dẫn người dùng (tiếng Việt) |

Stack: Go `1.27.3`, `github.com/go-rod/rod v0.116.2`, `github.com/mark3labs/mcp-go v1.4.0`. Package duy nhất là `package main`.

### 1.2 Hành vi hiện tại của 12 tool (KHÔNG được đổi tên, tham số, hay định dạng phản hồi)

`browser_new_tab(url)`, `browser_list_tabs()`, `browser_close_tab(tab_id)`, `browser_navigate(url, tab_id)`, `browser_snapshot(tab_id)`, `browser_click(selector, tab_id)`, `browser_type(selector, text, press_enter, tab_id)`, `browser_press_key(key, tab_id)`, `browser_screenshot(filename, tab_id)`, `browser_evaluate(script, tab_id)`, `get_browser_status()`, `browser_close()`.

Quy ước đang có, phải giữ:
- `tab_id` có dạng `tab-1`, `tab-2`... (bộ đếm tăng dần toàn cục). Bỏ trống `tab_id` thì dùng tab active.
- Phản hồi thành công bắt đầu bằng `✅ ...` (hoặc `📸` cho screenshot) kèm "summary" từ `getPageSummary`: `**Tab ID**`, `**Current URL**`, `**Page Title**`, `**Interactive Elements**` (khối code).
- Sau `navigate`: chờ load rồi `sleep 1s`. Sau `click`: `scrollIntoView`, `sleep 200ms`, click, `sleep 1s`, trả summary. Sau `press_key`: `sleep 500ms`. Sau `type` có `press_enter`: `sleep 1s`. **Giữ các thời gian chờ này trong Go**, không chuyển vào extension.
- Lỗi trả bằng `mcp.NewToolResultError(...)`, không panic.
- `findElement` thử 4 bước theo thứ tự, mỗi bước timeout 2s: (1) selector CSS/XPath thẳng; (2) `button, a, [role='button'], [role='link']` có text khớp (regex `ElementR`); (3) `[aria-label*="x"], [placeholder*="x"], input[name="x"]`; (4) bất kỳ phần tử có text khớp. Có xử lý `:has-text("...")` kiểu Playwright. **Đây là hành vi chuẩn mà bản JS phải tái tạo.**
- `jsExtractElements` (hằng số trong `main.go`) trích tối đa 100 phần tử tương tác có kích thước > 0, định dạng `- [role] "text" (id="" name="" type="")`. **Bản JS của extension dùng đúng logic đó.**

### 1.3 Vấn đề bảo mật đã phát hiện, phải sửa ở Phase 0

`main()` đang bind `addr := ":" + *port`, tức **lắng nghe mọi interface mạng**, không có xác thực. Ai cùng mạng LAN đều có thể gọi `browser_evaluate` điều khiển Chrome đang đăng nhập của chủ máy. Phase 0 phải sửa:

1. Chỉ bind loopback. **Lưu ý IPv4/IPv6:** `install.sh` cấu hình client bằng `http://localhost:3025/mcp`, mà `localhost` có thể phân giải ra `::1` trước `127.0.0.1`. Nếu chỉ bind `127.0.0.1` thì một số client sẽ hỏng. Vì vậy phải **mở hai listener**: `127.0.0.1:PORT` và `[::1]:PORT` (bỏ qua lỗi nếu máy không có IPv6), cùng phục vụ một `http.Server` qua `Serve(listener)`. Cờ `-host` (mặc định rỗng = hai loopback ở trên) cho phép ghi đè bằng một địa chỉ cụ thể.
2. Thư viện `mcp-go v1.4.0` (đã kiểm tra trong `streamable_http.go`) **đã** từ chối `Host` lạ khi kết nối đến từ loopback (chống DNS rebinding) và **không** phát header CORS khi không cấu hình origin. Dòng `server.WithStreamableHTTPCORS()` hiện tại không bật origin nào nên vô hại, nhưng nên giữ nguyên, không thêm `WithCORSAllowedOrigins`.
3. Thêm lớp phòng thủ bổ sung cho `/mcp` và `/sse`: nếu request có header `Origin` không rỗng thì trả 403, trừ khi origin nằm trong biến môi trường `BROWSERUSE_ALLOWED_ORIGINS` (danh sách cách nhau dấu phẩy). Client CLI (Claude Code, AGY...) không gửi `Origin`, nên không bị ảnh hưởng. Mục đích: một trang web trong trình duyệt không gọi được MCP.

### 1.4 Quy ước làm việc

- Chủ repo trao đổi bằng tiếng Việt; README và thông báo cho người dùng viết tiếng Việt, **comment và tên trong code viết tiếng Anh** (khớp code hiện có).
- Commit theo conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`), **một commit cho mỗi phase hoặc nhỏ hơn**. **Không push** khi chưa được chủ repo cho phép.
- Không thêm thư viện ngoài danh sách ở mục 4 mà không ghi lý do.
- Không commit bí mật (token, mật khẩu, email thật) vào repo.

---

## 2. Kiến trúc đã chốt

```
Agent(s) --MCP (HTTP :3025/mcp)--> daemon Go --WS 127.0.0.1:3025/ext--> extension (profile "work")
                                         \---WS 127.0.0.1:3025/ext--> extension (profile "personal")
                                         \---(dự phòng) rod/CDP ------> Chrome 9222 hoặc headless
```

### Quyết định then chốt

1. **Extension là "driver nguyên thủy", không phải relay CDP.** Extension chỉ thực hiện các thao tác nhỏ (tạo tab, chạy script, click...) và trả dữ liệu thô. Toàn bộ điều phối, thời gian chờ, định dạng văn bản trả cho Agent nằm ở Go. Lý do: relay CDP phải giả lập miền `Target.*` của rod, rủi ro cao và khó kiểm thử.
2. **Tách `Driver` interface** trong Go với hai bản cài đặt: `rodDriver` (code hiện có, chuyển nguyên) và `extDriver` (mới). Các tool chỉ nói chuyện với một router, router chọn driver.
3. **`tab_id` vẫn là `tab-N` toàn cục**, router giữ bảng `tab-N -> {driver, handle, profile}`. Nhờ vậy mọi tool ngoài `browser_new_tab` suy ra profile từ `tab_id`, Agent cũ không phải đổi gì.
4. **Chọn profile chỉ ở `browser_new_tab`** qua tham số `profile` (tuỳ chọn):
   - Có `profile`: dùng đúng profile đó (lỗi nếu offline).
   - Bỏ trống, đúng 1 profile extension online: dùng profile đó.
   - Bỏ trống, ≥2 profile online: **trả lỗi** liệt kê các profile, yêu cầu Agent chọn (không tự đoán).
   - Bỏ trống, 0 profile extension online: **dự phòng sang rod** như hiện tại.
5. **Xác thực**: token chung + kiểm tra `Origin` + chỉ nhận từ loopback (mục 5.4).
6. **Backend rod giữ nguyên.** Không xoá, không đổi hành vi khi không có extension.

---

## 3. Cấu trúc file mục tiêu

```
browseruse/
├── main.go                # giữ: flag, setupMCPServer, định nghĩa tool (gọi router)
├── router.go              # mới: bảng tab-N -> driver, chọn profile, activeTab, counter
├── driver.go              # mới: interface Driver + kiểu dữ liệu chung
├── driver_rod.go          # mới: chuyển code rod hiện có từ main.go sang (không đổi hành vi)
├── driver_ext.go          # mới: Driver gọi sang extension qua hub
├── ext_hub.go             # mới: WebSocket server /ext, registry profile, request/response
├── auth.go                # mới: sinh/đọc token, kiểm tra Origin/loopback
├── ext_hub_test.go        # mới: test hub bằng client giả lập extension
├── router_test.go         # mới: test chọn profile
├── main_test.go           # giữ, cập nhật nếu đổi tên hàm
└── extension/
    ├── manifest.json
    ├── background.js      # service worker: WS client, dispatcher, giữ kết nối
    ├── lib/
    │   ├── find_element.js    # bản JS của findElement (mục 6.3)
    │   └── snapshot.js        # bản JS của jsExtractElements (copy nguyên logic)
    ├── options.html
    └── options.js         # đặt nhãn profile + dán token + địa chỉ daemon
```

---

## 4. Phụ thuộc

- Go: thêm `github.com/coder/websocket` (WebSocket server cho `/ext`). Không thêm gì khác.
- Extension: **không dùng bundler, không npm**. JS thuần ES module, nạp trực tiếp.

---

## 5. Giao thức daemon <-> extension

Một kết nối WebSocket cho mỗi profile, JSON văn bản, mỗi khung là một object.

### 5.1 Bắt tay

Extension mở `ws://127.0.0.1:3025/ext` (địa chỉ cấu hình được trong options). Khung đầu tiên extension gửi:

```json
{"type":"hello","protocol":1,"token":"<token>","profile":"work","email":"a@b.com","instanceId":"<uuid lưu trong storage.local>","version":"0.1.0"}
```

Daemon đáp:

```json
{"type":"hello_ok","profile":"work"}
```

hoặc lỗi rồi đóng kết nối:

```json
{"type":"hello_error","code":"bad_token | label_in_use | bad_label | bad_protocol","message":"..."}
```

- `profile` (nhãn) khớp regex `^[a-z0-9][a-z0-9_-]{0,31}$`. Sai -> `bad_label`.
- Hai kết nối cùng nhãn: nếu `instanceId` giống nhau thì **thay kết nối cũ** (extension tự kết nối lại), nếu khác thì từ chối `label_in_use`.
- `protocol` là số nguyên phiên bản giao thức, hiện là `1`. Daemon từ chối khác phiên bản bằng `bad_protocol` (thông điệp nói rõ cần cập nhật daemon hay extension). `version` chỉ để hiển thị.
- `email` chỉ để hiển thị (có thể rỗng), không dùng để định danh.

### 5.2 Lệnh (daemon -> extension) và đáp (extension -> daemon)

```json
{"type":"req","id":17,"method":"tabs.create","params":{"url":"https://example.com"}}
{"type":"res","id":17,"result":{"tabId":123,"url":"https://example.com/","title":"Example Domain"}}
{"type":"res","id":18,"error":{"code":"element_not_found","message":"no element found matching 'Login'"}}
```

**Giới hạn kích thước khung:** `coder/websocket` mặc định giới hạn đọc **32 KiB**, ảnh chụp màn hình base64 sẽ vượt xa. Daemon **bắt buộc** gọi `conn.SetReadLimit(32 << 20)` (32 MiB). Ghi đồng thời từ nhiều goroutine vào cùng `Conn` là an toàn với thư viện này; phần đọc chỉ do một goroutine đảm nhiệm.

`id` là số nguyên tăng dần do daemon cấp, extension trả đúng `id`. Mặc định mỗi lệnh timeout **30 giây** ở daemon, và **huỷ ngay khi `ctx` của request MCP bị huỷ**; kết nối đứt thì mọi lệnh đang chờ trả lỗi `profile 'work' disconnected`.

### 5.3 Danh sách `method` (đủ cho 12 tool)

| method | params | result | Dùng cho tool |
| :--- | :--- | :--- | :--- |
| `tabs.create` | `{url?}` | `{tabId,url,title}` | `browser_new_tab` |
| `tabs.list` | `{tabIds:[int]}` | `[{tabId,url,title}]` | `browser_list_tabs`, status |
| `tabs.close` | `{tabId}` | `{}` | `browser_close_tab`, `browser_close` |
| `page.navigate` | `{tabId,url}` | `{url,title}` (đã chờ `status=complete`, tối đa 30s) | `browser_navigate` |
| `page.snapshot` | `{tabId}` | `{url,title,elements}` (`elements` là chuỗi, đúng định dạng `jsExtractElements`) | mọi tool cần summary |
| `page.click` | `{tabId,selector}` | `{}` | `browser_click` |
| `page.type` | `{tabId,selector,text}` | `{}` | `browser_type` |
| `page.pressKey` | `{tabId,key}` | `{}` | `browser_press_key`, `press_enter` của type |
| `page.screenshot` | `{tabId}` | `{pngBase64}` | `browser_screenshot` (daemon ghi file) |
| `page.evaluate` | `{tabId,script}` | `{value}` (JSON-serializable) | `browser_evaluate` |

Mã lỗi chuẩn: `tab_not_found`, `element_not_found`, `debugger_unavailable`, `script_error`, `timeout`, `restricted_page` (tab `chrome://`, Web Store...), `internal`.

### 5.4 Bảo mật endpoint `/ext` (bắt buộc)

1. Chỉ chấp nhận `RemoteAddr` là loopback; ngược lại trả 403.
2. Header `Origin` phải bắt đầu bằng `chrome-extension://`; ngược lại 403. (Trang web thường **không** được gọi được endpoint này.)
3. Khung `hello` phải có `token` đúng, so sánh bằng `subtle.ConstantTimeCompare`.
4. Daemon ghi log (stderr, **không** kèm token) mỗi lần profile kết nối, thay thế, ngắt kết nối.
5. Token: 32 byte ngẫu nhiên mã hoá hex, lưu ở `~/.browseruse/token` (quyền `0600`, thư mục `0700`), tạo khi daemon khởi động lần đầu. Có lệnh `make token` in ra token để dán vào options của extension.
6. Không ghi token vào log.

---

## 6. Hợp đồng chi tiết từng thành phần

### 6.1 `Driver` interface (Go, `driver.go`)

Gợi ý chữ ký; AGY được tinh chỉnh miễn giữ ý nghĩa:

```go
type PageSummary struct{ URL, Title, Elements string }

type Driver interface {
    Name() string                                  // "rod" | "ext:<profile>"
    NewTab(ctx context.Context, url string) (handle string, err error)
    ListTabs(ctx context.Context, handles []string) ([]TabInfo, error)
    CloseTab(ctx context.Context, handle string) error
    Navigate(ctx context.Context, handle, url string) error
    Snapshot(ctx context.Context, handle string) (PageSummary, error)
    Click(ctx context.Context, handle, selector string) error
    Type(ctx context.Context, handle, selector, text string) error
    PressKey(ctx context.Context, handle, key string) error
    Screenshot(ctx context.Context, handle string) ([]byte, error)
    Evaluate(ctx context.Context, handle, script string) (any, error)
}
```

`getPageSummary` ở tầng router/tool định dạng `PageSummary` thành chuỗi cũ, nên đầu ra cho Agent không đổi.

### 6.2 Router (`router.go`)

- Giữ `counter`, `activeTab`, `mu`, bảng `tabs map[string]tabRef`.
- `NewTab(url, profile)` theo quy tắc mục 2.4.
- `GetTab(tabID)` giữ logic hiện có (tab_id tường minh -> tab active -> tab đầu tiên -> lỗi/tạo mới). Khi tab thuộc profile đã offline: dọn khỏi bảng và trả lỗi rõ ràng.
- Hành vi "đóng tab cuối thì tắt browser tự launch" chỉ áp dụng cho `rodDriver` ở chế độ tự launch (đã có trong code hiện tại, phải giữ).
- Hành vi "browser chết thì bỏ handle và launch lại" của rod (đã có) phải giữ.

### 6.3 Extension

**manifest.json**: MV3, `background.service_worker = "background.js"` với `"type":"module"`, `options_page = "options.html"`. Quyền: `tabs`, `scripting`, `debugger`, `storage`, `alarms`, `identity`, `identity.email`. `host_permissions: ["<all_urls>"]`. Không khai báo `content_scripts` (chèn script theo yêu cầu bằng `chrome.scripting.executeScript`).

**Kết nối và giữ sống**
- Đọc cấu hình từ `chrome.storage.local`: `label`, `token`, `daemonUrl` (mặc định `ws://127.0.0.1:3025/ext`), `instanceId` (sinh UUID lần đầu). Thiếu `label` hoặc `token` thì không kết nối, badge hiện `!`.
- Kết nối lại với backoff 1s, 2s, 4s ... tối đa 30s, reset khi thành công.
- Giữ service worker sống: gửi khung `{"type":"ping"}` mỗi 20s (daemon đáp `pong`), cộng thêm `chrome.alarms` mỗi 30s để đánh thức và kiểm tra kết nối nếu service worker đã ngủ. **Cần kiểm chứng bằng thực nghiệm** service worker không bị ngủ khi nhàn rỗi 5 phút (mục 9).
- Badge: `ON` xanh khi đã `hello_ok`, `OFF` khi mất kết nối.

**Cách thực thi từng method**
- **Cửa sổ làm việc (quyết định thiết kế):** mặc định extension mở tab của Agent trong **một cửa sổ riêng của profile** (tạo lười bằng `chrome.windows.create({focused:false})`, nhớ `windowId`; nếu cửa sổ bị người dùng đóng thì lần sau tạo lại). Lý do: tab nền (`active:false`) trong cửa sổ đang dùng có thể không vẽ/không nhận sự kiện đúng (xem rủi ro "tab nền" ở mục 9), còn việc kích hoạt tab của Agent trong cửa sổ người dùng sẽ làm nhảy tab họ đang xem. Trong cửa sổ riêng, mỗi tab được `chrome.tabs.update(tabId,{active:true})` trước khi thao tác nên không ảnh hưởng tab người dùng. Options page có chọn `windowMode`: `dedicated` (mặc định) hoặc `current` (mở trong cửa sổ đang focus của profile). **Phase 3 phải chạy spike kiểm chứng** (mục 7) trước khi chốt mặc định.
- `tabs.create`: tạo tab trong cửa sổ làm việc theo `windowMode`. Trả `{tabId,url,title}` **sau khi** `status==='complete'` nếu có `url` (tối đa 30s).
- `page.navigate`: `chrome.tabs.update` rồi chờ `onUpdated` với `status==='complete'`, timeout 30s.
- `page.snapshot`: `chrome.scripting.executeScript({target:{tabId}, func: snapshotFn})` trả `{url,title,elements}`. `snapshotFn` copy nguyên logic `jsExtractElements`.
- `page.click`: chèn script `findElement` (bản JS, xem dưới) để tìm phần tử, `scrollIntoView({block:'center'})`, lấy toạ độ giữa phần tử (trừ offset của iframe nếu có). Sau đó **ưu tiên click bằng `chrome.debugger`** (`Input.dispatchMouseEvent`: `mouseMoved`, `mousePressed`, `mouseReleased`, `clickCount:1`) để sự kiện là "trusted"; nếu gắn debugger không được thì **dự phòng** `el.click()`.
- `page.type`: tìm phần tử, `el.focus()`, **xoá nội dung cũ** (với `input`/`textarea`: `el.select()`; với `contenteditable`: chọn toàn bộ con bằng `Selection.selectAllChildren`), rồi `Input.insertText` qua debugger (thay thế vùng chọn). Dự phòng khi không có debugger: gán `value` rồi phát sự kiện `input` và `change` (bubbles). Hành vi khớp tool hiện tại: ghi đè nội dung cũ, không nối thêm.
- `page.pressKey`: `Input.dispatchKeyEvent` (keyDown rồi keyUp) qua debugger. Hỗ trợ tối thiểu: `enter`, `tab`, `escape`/`esc`, `backspace`, `arrowdown`/`down`, `arrowup`/`up` (đúng như tool hiện tại; key lạ thì mặc định Enter như code hiện tại).
- `page.screenshot`: `Page.captureScreenshot` qua debugger (không cần tab đang active), trả base64.
- `page.evaluate`: chạy trong world MAIN, `returnByValue:true`, lỗi script trả `script_error`. **Tương thích với rod (đã kiểm tra mã nguồn `rod v0.116.2`, `page_eval.go`):** rod bọc `script` thành `(script).apply(this, arguments)`, tức tool hiện tại **chỉ chạy đúng khi `script` là một biểu thức hàm** (ví dụ `() => document.title`); chuỗi như `document.title` sẽ lỗi. Rod cũng **không** `await` Promise. Quyết định cho extension: nhận **cả hai dạng**. Nếu `script` khớp regex `^\s*(async\s+)?(function\b|\([^)]*\)\s*=>|[A-Za-z_$][\w$]*\s*=>)` thì coi là hàm và gọi nó (`await` kết quả nếu là Promise); ngược lại coi là biểu thức và `Runtime.evaluate`. Đây là tập cha của hành vi rod (không phá cách dùng cũ). Ghi rõ điều này trong mô tả tool `browser_evaluate` và README.
- Quản lý debugger: `chrome.debugger.attach({tabId},"1.3")` lười (lần đầu cần), lưu tập tab đã gắn; `detach` khi tab đóng (`tabs.onRemoved`) hoặc `debugger.onDetach`. Nếu `attach` lỗi (DevTools đang mở, trang bị hạn chế) thì trả `debugger_unavailable` hoặc dùng đường dự phòng ở trên.
- Trang bị hạn chế (`chrome://`, `chrome-extension://`, Chrome Web Store): trả `restricted_page`.

**`lib/find_element.js`** (chạy trong trang, tái tạo `findElement`):
1. Chuẩn hoá `target = trim`, `clean = bỏ dấu nháy ở hai đầu`; nếu có `:has-text(` thì `clean` = nội dung trong ngoặc, đã bỏ nháy.
2. Thử `document.querySelector(target)` (bắt lỗi cú pháp) rồi XPath nếu `target` bắt đầu bằng `/` hoặc `(`.
3. Trong `button, a, [role='button'], [role='link']`: phần tử đầu tiên có `innerText` khớp regex `clean` (không phân biệt hoa thường; nếu `clean` không phải regex hợp lệ thì khớp chuỗi con).
4. `[aria-label*="clean"], [placeholder*="clean"], input[name="clean"]` (escape dấu nháy trong `clean`).
5. Mọi phần tử có text khớp, **ưu tiên phần tử sâu nhất** (không có con nào khớp) để không chọn `html`/`body`.
6. Chỉ nhận phần tử có `getBoundingClientRect()` rộng và cao > 0 (nhìn thấy). Không có thì lỗi `element_not_found`.
(Bản rod thử lần lượt từng bước với timeout 2s do trang có thể chưa render xong. Bản JS cần **thăm dò lại mỗi 200ms trong tối đa 2s** cho mỗi lần tìm, rồi mới kết luận không thấy, để tương đương.)

**Giới hạn đã biết (chấp nhận, ghi vào README):** chỉ thao tác trên **khung (frame) trên cùng**, không chui vào iframe (backend rod cũng vậy); không xử lý hộp thoại `alert/confirm/prompt` (không làm tab bị treo vĩnh viễn: lệnh gặp hộp thoại sẽ hết 30s và trả `timeout`); tab Agent đã mở vẫn còn trong Chrome nếu daemon khởi động lại (bảng `tab-N` bị mất, tab mồ côi do người dùng tự đóng).

**Options page**: ô nhập `label`, `token`, `daemonUrl`; nút Lưu (validate regex nhãn); hiển thị trạng thái kết nối, email Chrome (nếu có), và `instanceId` (chỉ đọc). Lưu xong thì kết nối lại ngay.

### 6.4 Tool mới và tool đổi

- **Tool mới `list_profiles()`**: trả danh sách profile extension đang online: nhãn, email (nếu có), số tab đang quản lý, thời điểm kết nối. Không có profile nào thì nói rõ đang dùng backend rod.
- `browser_new_tab`: thêm tham số tuỳ chọn `profile` (string). Mô tả tool nhắc Agent gọi `list_profiles` khi có nhiều profile.
- `browser_list_tabs` và `get_browser_status`: thêm cột/dòng cho biết mỗi tab thuộc profile/driver nào. `get_browser_status` hiện cả backend rod lẫn các profile extension.
- `browser_close`: đóng mọi tab mà **daemon đã mở** (kể cả của extension); **không đóng cửa sổ hay tab người dùng tự mở**.
- Mô tả (`WithDescription`) của các tool giữ nguyên, trừ các thay đổi trên.

---

## 7. Kế hoạch theo phase

Mỗi phase kết thúc bằng một commit, các test của phase đó phải xanh, và `go vet ./...` sạch. Làm **tuần tự**, đừng gộp phase.

### Phase 0: Chuẩn bị (nền móng)
- [x] Sửa `Makefile` (`build`) và `install.sh` để build bằng `go build -o bin/browseruse .` (nhiều file `.go`).
- [x] Bind loopback bằng **hai listener** `127.0.0.1` và `[::1]` (mục 1.3), cờ `-host` để ghi đè; thêm kiểm tra `Origin` cho `/mcp` và `/sse` (mục 1.3).
- [x] Thêm `auth.go`: sinh và đọc token (mục 5.4), lệnh `make token`.
- [x] Cập nhật README mục xử lý sự cố nếu đổi cách build.
- **Nghiệm thu**: `make build`, `make restart`, `make status` chạy; `lsof -nP -iTCP:3025 -sTCP:LISTEN` chỉ thấy `127.0.0.1` và/hoặc `[::1]`, **không** thấy `*:3025`; `curl http://localhost:3025/mcp` và `curl http://127.0.0.1:3025/mcp` đều tới được server; `curl -H 'Origin: https://evil.example' -X POST http://127.0.0.1:3025/mcp` trả 403; MCP gọi `get_browser_status` vẫn chạy với cấu hình `http://localhost:3025/mcp` cũ. Từ máy khác trong LAN (hoặc `curl` tới IP LAN của máy) kết nối bị từ chối.

### Phase 1: Tách `Driver` (refactor thuần, không đổi hành vi)
- [ ] Tạo `driver.go`, `driver_rod.go`, `router.go`; chuyển logic `BrowserManager`, `findElement`, kết nối CDP/launch headless sang `rodDriver`.
- [ ] Các tool trong `main.go` gọi router.
- [ ] Giữ nguyên đầu ra văn bản của 12 tool.
- [ ] **Trước khi refactor**, viết bộ test đặc tả (characterization test) cho `rodDriver` trên trang `data:` URL cục bộ (không cần mạng): `findElement` với 4 dạng selector, `evaluate` dạng hàm và dạng biểu thức, `type` ghi đè nội dung cũ, `snapshot` đúng định dạng. Bộ test này là **chuẩn đối chiếu** để kiểm tra extension ở Phase 4 cho cùng kết quả.
- **Nghiệm thu**: `go build .`, `go vet ./...`; `go test ./...` giữ kết quả như trước (các test cần mạng được phép bỏ qua bằng `t.Skip` khi không có mạng, **không** đổi ý nghĩa); chạy tay 12 tool qua MCP, kết quả giống trước.

### Phase 2: Hub + giao thức (chưa cần Chrome thật)
- [ ] `ext_hub.go`: endpoint `/ext`, kiểm tra loopback/Origin/token, registry profile, `req/res` với `id`, timeout 30s, huỷ lệnh khi mất kết nối, `ping/pong`.
- [ ] `driver_ext.go` gọi qua hub; router biết chọn profile (mục 2.4).
- [ ] `ext_hub_test.go`: viết **client giả lập extension** trong Go (dùng `coder/websocket`) để kiểm thử.
- **Nghiệm thu bằng test tự động** (bắt buộc có):
  - token sai, Origin không phải `chrome-extension://`, nhãn sai -> bị từ chối đúng mã lỗi.
  - hai kết nối cùng nhãn: cùng `instanceId` thì thay, khác thì `label_in_use`.
  - lệnh đang chờ gặp mất kết nối -> lỗi `disconnected`, không treo.
  - lệnh quá 30s -> `timeout` (cho phép đặt timeout nhỏ trong test); `ctx` bị huỷ thì lệnh đang chờ kết thúc ngay.
  - khung phản hồi ~5 MB (giả lập ảnh chụp) được nhận đủ, không bị cắt hay ngắt kết nối.
  - `protocol` khác phiên bản -> `bad_protocol`.
  - router: 0/1/≥2 profile online cho đúng 3 hành vi ở mục 2.4.

### Phase 3: Extension bản tối thiểu
- [ ] **Spike trước (làm đầu tiên, ghi kết quả vào mục "Kết quả spike" cuối file này):** với một extension tối giản kết nối được daemon, kiểm chứng bằng thực nghiệm trên Chrome của chủ repo:
  1. Service worker còn kết nối sau 5 phút nhàn rỗi không? (ping 20s + alarm 30s)
  2. Trong **cửa sổ riêng**, `Page.captureScreenshot` và `Input.dispatchMouseEvent` hoạt động khi cửa sổ đó **không focus** và khi **bị cửa sổ khác che hoàn toàn**? Cũng thử với tab `active:false` trong cửa sổ người dùng để đối chiếu.
  3. `chrome.identity.getProfileUserInfo` trả email ở profile đã đăng nhập Chrome?
  4. Chrome có hiện cảnh báo/khoá extension unpacked không?
  Từ kết quả, chốt mặc định `windowMode` và ghi lại; nếu cả hai cách đều không đáng tin thì **dừng và hỏi chủ repo** trước khi làm tiếp.
- [ ] `manifest.json`, `background.js`, `options.html/js`, kết nối + giữ sống + `hello`.
- [ ] Hỗ trợ `tabs.create`, `tabs.list`, `tabs.close`, `page.navigate`, `page.snapshot`.
- [ ] `list_profiles` hoạt động.
- **Nghiệm thu thủ công** (ghi lại kết quả trong PR/commit): load unpacked vào 1 profile, đặt nhãn+token, `list_profiles` thấy profile; `browser_new_tab(url=example.com)` mở tab ở **đúng profile**; `browser_snapshot` trả đúng "Example Domain" và nút "Learn more".

### Phase 4: Tương tác
- [ ] `lib/find_element.js`, `page.click`, `page.type`, `page.pressKey`, `page.screenshot`, `page.evaluate`, quản lý debugger.
- **Đối chiếu với bộ test đặc tả của Phase 1:** chạy cùng các kịch bản (4 dạng selector, `evaluate` hàm và biểu thức, `type` ghi đè, định dạng `snapshot`) lên extension bằng một trang test cục bộ (`python3 -m http.server` phục vụ file HTML trong thư mục tạm, vì extension không đọc được `data:`/`file:` tuỳ cài đặt), kết quả phải khớp rod; ghi lại mọi khác biệt.
- **Nghiệm thu thủ công** trên `https://example.com`, một trang có form đăng nhập (vd trang test công khai), và một trang SPA: click link, gõ vào ô input + Enter, chụp ảnh và mở được file PNG, `evaluate("document.title")`.

### Phase 5: Nhiều profile song song
- [ ] Cài extension vào profile thứ hai với nhãn khác.
- [ ] Kiểm thử tab song song, tab không lẫn, `browser_close` chỉ đóng tab của daemon.
- **Nghiệm thu thủ công**: `list_profiles` thấy 2 profile; `new_tab` không `profile` -> lỗi liệt kê; `new_tab profile=work` và `profile=personal` mở đúng cửa sổ profile tương ứng; điều khiển xen kẽ hai tab; tắt cửa sổ một profile -> tab của profile đó báo lỗi rõ, profile kia vẫn chạy; bật lại thì tự kết nối lại.

### Phase 6: Tài liệu và dọn dẹp
- [ ] README: mục "Cài extension" (Developer mode -> Load unpacked -> chọn `extension/`, đặt nhãn, dán token từ `make token`), mục chọn profile, bảng so sánh backend, cập nhật phần xử lý sự cố. Bỏ yêu cầu Full Disk Access khỏi **đường khuyên dùng** nhưng giữ ghi chú cho đường CDP.
- [ ] Cập nhật mục "Server lấy Chrome từ đâu" trong README (thêm backend extension ở ưu tiên 1).
- [ ] Cập nhật `PLAN.md`: đánh dấu hoàn thành, ghi lại các điều học được.

---

## 8. Việc KHÔNG làm (ngoài phạm vi)

- Không xoá backend rod/CDP và code đọc `DevToolsActivePort`.
- Không đổi tên tool, tham số hiện có, hay định dạng văn bản trả về.
- Không đóng gói lên Chrome Web Store, không làm bản ký số.
- Không hỗ trợ Firefox/Safari.
- Không hỗ trợ điều khiển tab do người dùng tự mở (chưa có cơ chế "adopt tab"; để dành sau).
- Không làm tab group, không làm giao diện đẹp cho options.
- Không hỗ trợ iframe, hộp thoại JS (`alert/confirm/prompt`), kéo thả, tải lên file.
- Không tự thêm xác thực cho endpoint `/mcp` (ngoài phạm vi; bind `127.0.0.1` ở Phase 0 là đủ cho lần này).

## 9. Rủi ro và điểm phải kiểm chứng sớm

| Rủi ro | Cách kiểm chứng / ứng phó |
| :--- | :--- |
| Service worker MV3 bị ngủ làm rớt kết nối | Ở Phase 3, để nhàn rỗi 5 phút rồi gọi lệnh; nếu rớt thì tăng tần suất ping/alarm hoặc kết nối lại tự động. Ghi kết quả vào `PLAN.md` |
| Thanh vàng "started debugging this browser" | Chấp nhận. Ghi trong README |
| `chrome.debugger.attach` thất bại khi DevTools đang mở trên tab | Dùng đường dự phòng `el.click()`/`value`; screenshot/evaluate thì báo `debugger_unavailable` kèm gợi ý đóng DevTools |
| Chrome nhắc tắt extension chưa đóng gói | Ghi trong README, không xử lý |
| `findElement` bản JS lệch hành vi so với rod | Viết bộ test thủ công ở Phase 4 trên cùng một trang cho cả hai backend và so kết quả |
| Giữ `tab-N` toàn cục khi profile offline rồi online lại | Tab cũ của profile đó coi là mất; router dọn bảng khi hub báo ngắt kết nối |
| **Tab nền không vẽ/nhận sự kiện:** `Page.captureScreenshot` và input qua CDP trên tab không hiển thị có thể treo hoặc bị bỏ qua | Dùng cửa sổ riêng + kích hoạt tab trước khi thao tác; kiểm chứng ở spike Phase 3; phương án cuối là `chrome.tabs.captureVisibleTab` sau khi kích hoạt |
| Tự động hoá E2E bằng cách nạp extension qua cờ `--load-extension` | Có thể bị vô hiệu trên Chrome bản chính thức từ khoảng v137 (chưa kiểm chứng ở máy này). Không dựa vào nó; E2E extension là thủ công. Tuỳ chọn: dùng Chrome for Testing |
| Ảnh chụp lớn vượt giới hạn WebSocket 32 KiB mặc định | `SetReadLimit(32<<20)` (mục 5.2), có test gửi khung ~5 MB |
| Chỉ có thể test E2E thật trên máy chủ repo (macOS, nhiều profile) | Phần tự động hoá giới hạn ở hub/router (Phase 2); phần extension nghiệm thu thủ công, ghi rõ kết quả |

## 10. Tiêu chí hoàn thành toàn bộ

1. Không cần Full Disk Access và không đọc `DevToolsActivePort` ở đường extension.
2. `list_profiles` hiện đúng các profile đang online với nhãn đã đặt.
3. `browser_new_tab` với `profile` mở tab đúng profile; không `profile` khi có ≥2 profile thì báo lỗi hữu ích.
4. Hai profile điều khiển song song, tab không lẫn nhau.
5. 12 tool cũ chạy như trước khi không có extension (backend rod).
6. `/ext` chỉ chấp nhận loopback + Origin `chrome-extension://` + token đúng; `:3025` chỉ bind `127.0.0.1`.
7. `go build .`, `go vet ./...`, `go test ./...` (không tính test cần mạng) đều xanh.
8. README hướng dẫn cài extension và chọn profile; `PLAN.md` được cập nhật trạng thái.

## 11. Cách báo cáo khi xong mỗi phase

Gửi cho chủ repo: danh sách commit, kết quả các lệnh nghiệm thu (dán đầu ra), những gì đã kiểm chứng tay và những gì **chưa** kiểm chứng, và mọi chỗ làm khác với tài liệu này kèm lý do. Gặp điểm không rõ trong tài liệu thì **hỏi trước khi đoán**, đừng đổi hợp đồng ở mục 5 hay 6 một mình.

## 12. Kết quả spike (AGY điền ở Phase 3)

| Câu hỏi | Kết quả | Quyết định |
| :--- | :--- | :--- |
| Service worker sống sau 5 phút nhàn rỗi? | _chưa kiểm chứng_ | |
| Screenshot/click trong cửa sổ riêng khi không focus / bị che? | _chưa kiểm chứng_ | |
| Email Chrome đọc được? | _chưa kiểm chứng_ | |
| Chrome có khoá extension unpacked? | _chưa kiểm chứng_ | |
| `windowMode` mặc định chốt là | _chưa chốt_ | |

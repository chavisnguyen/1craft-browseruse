package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	ProtocolVersion = 1
	MaxMessageSize  = 32 << 20 // 32 MiB read limit for base64 screenshots
)

var labelRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type ProfileInfo struct {
	Profile     string    `json:"profile"`
	Email       string    `json:"email,omitempty"`
	InstanceID  string    `json:"instance_id"`
	Version     string    `json:"version,omitempty"`
	ConnectedAt time.Time `json:"connected_at"`
	OpenTabs    int       `json:"open_tabs"`
}

type HelloMessage struct {
	Type       string `json:"type"`
	Protocol   int    `json:"protocol"`
	Token      string `json:"token"`
	Profile    string `json:"profile"`
	Label      string `json:"label"` // Support "label" per PLAN.md Section 5.1
	Email      string `json:"email,omitempty"`
	InstanceID string `json:"instanceId"`
	Version    string `json:"version,omitempty"`
}

type HelloOkMessage struct {
	Type    string `json:"type"`
	Profile string `json:"profile"`
}

type HelloErrorMessage struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RequestMessage struct {
	Type   string `json:"type"`
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type ResponseMessage struct {
	Type   string          `json:"type"`
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

type resOrErr struct {
	result json.RawMessage
	err    error
}

type ExtensionSession struct {
	profile     string
	email       string
	instanceID  string
	version     string
	connectedAt time.Time
	conn        *websocket.Conn
	hub         *ExtHub

	mu        sync.Mutex
	reqSeq    int64
	pending   map[int64]chan resOrErr
	closeChan chan struct{}
	closeOnce sync.Once
}

func newExtensionSession(msg HelloMessage, conn *websocket.Conn, hub *ExtHub) *ExtensionSession {
	return &ExtensionSession{
		profile:     msg.Profile,
		email:       msg.Email,
		instanceID:  msg.InstanceID,
		version:     msg.Version,
		connectedAt: time.Now(),
		conn:        conn,
		hub:         hub,
		pending:     make(map[int64]chan resOrErr),
		closeChan:   make(chan struct{}),
	}
}

func (s *ExtensionSession) Close() {
	s.closeOnce.Do(func() {
		close(s.closeChan)
		_ = s.conn.CloseNow()

		s.mu.Lock()
		defer s.mu.Unlock()
		for id, ch := range s.pending {
			ch <- resOrErr{err: fmt.Errorf("profile '%s' disconnected", s.profile)}
			close(ch)
			delete(s.pending, id)
		}
	})
}

func (s *ExtensionSession) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	reqID := atomic.AddInt64(&s.reqSeq, 1)
	ch := make(chan resOrErr, 1)

	s.mu.Lock()
	select {
	case <-s.closeChan:
		s.mu.Unlock()
		return nil, fmt.Errorf("profile '%s' disconnected", s.profile)
	default:
		s.pending[reqID] = ch
		s.mu.Unlock()
	}

	defer func() {
		s.mu.Lock()
		delete(s.pending, reqID)
		s.mu.Unlock()
	}()

	req := RequestMessage{
		Type:   "req",
		ID:     reqID,
		Method: method,
		Params: params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	writeCtx, cancelWrite := context.WithTimeout(ctx, 10*time.Second)
	defer cancelWrite()

	if err := s.conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("failed to write request to profile '%s': %w", s.profile, err)
	}

	timeout := s.hub.defaultTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closeChan:
		return nil, fmt.Errorf("profile '%s' disconnected", s.profile)
	case <-time.After(timeout):
		return nil, errors.New("timeout")
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return res.result, nil
	}
}

type ExtHub struct {
	mu             sync.RWMutex
	sessions       map[string]*ExtensionSession
	defaultTimeout time.Duration
	router         *Router
}

func NewExtHub(defaultTimeout time.Duration) *ExtHub {
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	return &ExtHub{
		sessions:       make(map[string]*ExtensionSession),
		defaultTimeout: defaultTimeout,
	}
}

func (h *ExtHub) SetRouter(r *Router) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.router = r
}

func (h *ExtHub) GetSession(profile string) (*ExtensionSession, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[profile]
	return s, ok
}

func (h *ExtHub) OnlineProfiles() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var list []string
	for p := range h.sessions {
		list = append(list, p)
	}
	return list
}

func (h *ExtHub) ListProfiles() []ProfileInfo {
	h.mu.RLock()
	sessions := make([]*ExtensionSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	router := h.router
	h.mu.RUnlock()

	var result []ProfileInfo
	for _, s := range sessions {
		openTabs := 0
		if router != nil {
			openTabs = router.countTabsForProfile(s.profile)
		}
		result = append(result, ProfileInfo{
			Profile:     s.profile,
			Email:       s.email,
			InstanceID:  s.instanceID,
			Version:     s.version,
			ConnectedAt: s.connectedAt,
			OpenTabs:    openTabs,
		})
	}
	return result
}

func (h *ExtHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Loopback only
	if !IsLoopbackAddr(r.RemoteAddr) {
		http.Error(w, "Forbidden: non-loopback connection", http.StatusForbidden)
		return
	}

	// 2. Origin check: must begin with chrome-extension://
	origin := r.Header.Get("Origin")
	if !strings.HasPrefix(origin, "chrome-extension://") {
		http.Error(w, "Forbidden: invalid Origin (chrome-extension:// required)", http.StatusForbidden)
		return
	}

	// 3. Accept websocket connection
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // Already strictly checked above
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusInternalError, "")

	// 4. Set 32 MiB read limit for base64 screenshots
	conn.SetReadLimit(MaxMessageSize)

	// 5. Handshake: wait for hello frame with 5s timeout
	handshakeCtx, cancelHandshake := context.WithTimeout(r.Context(), 5*time.Second)
	msgType, data, err := conn.Read(handshakeCtx)
	cancelHandshake()

	if err != nil || msgType != websocket.MessageText {
		_ = h.sendHelloError(conn, "bad_protocol", "failed to read handshake frame")
		return
	}

	var hello HelloMessage
	if err := json.Unmarshal(data, &hello); err != nil || hello.Type != "hello" {
		_ = h.sendHelloError(conn, "bad_protocol", "invalid hello frame format")
		return
	}

	if hello.Profile == "" && hello.Label != "" {
		hello.Profile = hello.Label
	}

	// Protocol version check
	if hello.Protocol != ProtocolVersion {
		_ = h.sendHelloError(conn, "bad_protocol",
			fmt.Sprintf("protocol mismatch: daemon protocol %d, extension protocol %d. Please update daemon or extension.", ProtocolVersion, hello.Protocol))
		return
	}

	// Token validation
	expectedToken, err := GetOrCreateToken()
	if err != nil || expectedToken == "" || subtle.ConstantTimeCompare([]byte(expectedToken), []byte(strings.TrimSpace(hello.Token))) != 1 {
		_ = h.sendHelloError(conn, "bad_token", "invalid authentication token")
		return
	}

	// Profile label format check
	if !labelRegex.MatchString(hello.Profile) {
		_ = h.sendHelloError(conn, "bad_label", "profile label must match regex ^[a-z0-9][a-z0-9_-]{0,31}$")
		return
	}

	// Register session (handling duplicate labels)
	session := newExtensionSession(hello, conn, h)
	var oldSession *ExtensionSession

	h.mu.Lock()
	if existing, exists := h.sessions[hello.Profile]; exists {
		if existing.instanceID == hello.InstanceID {
			// Same instance reconnecting: replace old connection
			oldSession = existing
			h.sessions[hello.Profile] = session
		} else {
			// Different instance attempting to claim same label: reject
			h.mu.Unlock()
			_ = h.sendHelloError(conn, "label_in_use", fmt.Sprintf("profile '%s' is already in use by another instance", hello.Profile))
			return
		}
	} else {
		h.sessions[hello.Profile] = session
	}
	h.mu.Unlock()

	if oldSession != nil {
		fmt.Fprintf(os.Stderr, "ext_hub: profile '%s' replaced by new connection (instance: %s)\n", hello.Profile, hello.InstanceID)
		oldSession.Close()
	} else {
		fmt.Fprintf(os.Stderr, "ext_hub: profile '%s' connected (instance: %s)\n", hello.Profile, hello.InstanceID)
	}

	// Send hello_ok
	okMsg, _ := json.Marshal(HelloOkMessage{Type: "hello_ok", Profile: hello.Profile})
	if err := conn.Write(r.Context(), websocket.MessageText, okMsg); err != nil {
		h.removeSession(hello.Profile, session)
		return
	}

	// Reader loop
	defer func() {
		h.removeSession(hello.Profile, session)
		session.Close()
		fmt.Fprintf(os.Stderr, "ext_hub: profile '%s' disconnected\n", hello.Profile)
	}()

	for {
		typ, data, err := conn.Read(r.Context())
		if err != nil {
			break
		}
		if typ != websocket.MessageText {
			continue
		}

		// Peek message type
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(data, &generic); err != nil {
			continue
		}

		var msgTypeStr string
		if tVal, ok := generic["type"]; ok {
			_ = json.Unmarshal(tVal, &msgTypeStr)
		} else if _, hasID := generic["id"]; hasID {
			msgTypeStr = "res"
		}

		switch msgTypeStr {
		case "ping":
			pong, _ := json.Marshal(map[string]string{"type": "pong"})
			_ = conn.Write(r.Context(), websocket.MessageText, pong)

		case "res":
			var res ResponseMessage
			if err := json.Unmarshal(data, &res); err == nil {
				session.mu.Lock()
				ch, ok := session.pending[res.ID]
				session.mu.Unlock()

				if ok {
					if res.Error != nil {
						ch <- resOrErr{err: res.Error}
					} else {
						ch <- resOrErr{result: res.Result}
					}
				}
			}
		}
	}
}

func (h *ExtHub) removeSession(profile string, s *ExtensionSession) {
	h.mu.Lock()
	if current, ok := h.sessions[profile]; ok && current == s {
		delete(h.sessions, profile)
	}
	h.mu.Unlock()

	if h.router != nil {
		h.router.onProfileDisconnected(profile)
	}
}

func (h *ExtHub) sendHelloError(conn *websocket.Conn, code, message string) error {
	errMsg, _ := json.Marshal(HelloErrorMessage{
		Type:    "hello_error",
		Code:    code,
		Message: message,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, errMsg)
	return conn.CloseNow()
}

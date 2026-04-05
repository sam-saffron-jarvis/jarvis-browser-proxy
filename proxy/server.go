package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	Token         string
	BrowserCmd    string
	ChromeBaseURL string
}

type CommandResult struct {
	ReturnCode int
	Stdout     string
	Stderr     string
}

type ProcessManager interface {
	Run(action string) CommandResult
}

type ShellProcessManager struct {
	Command string
}

func (s ShellProcessManager) Run(action string) CommandResult {
	cmd := exec.Command(s.Command, action)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = 1
			if stderr.Len() == 0 {
				stderr.WriteString(err.Error())
			}
		}
	}
	return CommandResult{ReturnCode: code, Stdout: strings.TrimSpace(stdout.String()), Stderr: strings.TrimSpace(stderr.String())}
}

type Server struct {
	cfg      Config
	manager  ProcessManager
	client   *http.Client
	upgrader websocket.Upgrader
}

func NewServer(cfg Config, manager ProcessManager) *Server {
	if manager == nil {
		manager = ShellProcessManager{Command: cfg.BrowserCmd}
	}
	return &Server{
		cfg:     cfg,
		manager: manager,
		client:  &http.Client{Timeout: 5 * time.Second},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/jarvis-browser/status", s.requireAuth(s.handleCommand("status", http.MethodGet)))
	mux.HandleFunc("/jarvis-browser/start", s.requireAuth(s.handleCommand("start", http.MethodPost)))
	mux.HandleFunc("/jarvis-browser/stop", s.requireAuth(s.handleCommand("stop", http.MethodPost)))
	mux.HandleFunc("/jarvis-browser/restart", s.requireAuth(s.handleCommand("restart", http.MethodPost)))
	mux.HandleFunc("/jarvis-browser/json/", s.requireAuth(s.handleJSONProxy))
	mux.HandleFunc("/jarvis-browser/devtools/", s.handleWebsocketProxy)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" || extractToken(r) != s.cfg.Token {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleCommand(action, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		result := s.manager.Run(action)
		writeCommandResult(w, result)
	}
}

func (s *Server) handleJSONProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	endpoint := strings.TrimPrefix(r.URL.Path, "/jarvis-browser/json/")
	if endpoint != "version" && endpoint != "list" && endpoint != "protocol" && endpoint != "new" {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "unsupported chrome json endpoint"})
		return
	}

	resp, err := s.client.Get(strings.TrimRight(s.cfg.ChromeBaseURL, "/") + "/json/" + endpoint)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"detail": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"detail": fmt.Sprintf("chrome returned HTTP %d for /json/%s", resp.StatusCode, endpoint)})
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"detail": err.Error()})
		return
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"detail": "invalid JSON from chrome: " + err.Error()})
		return
	}

	rewritten := rewriteWebsocketURLs(payload, websocketProxyBaseURL(r))
	writeJSON(w, http.StatusOK, rewritten)
}

func (s *Server) handleWebsocketProxy(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Token == "" || extractToken(r) != s.cfg.Token {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "unauthorized"})
		return
	}
	devtoolsPath := strings.TrimPrefix(r.URL.Path, "/jarvis-browser/devtools/")
	upstreamURL := wsBaseURL(s.cfg.ChromeBaseURL) + "/" + devtoolsPath

	downstream, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer downstream.Close()

	upstream, _, err := websocket.DefaultDialer.Dial(upstreamURL, nil)
	if err != nil {
		_ = downstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()), time.Now().Add(time.Second))
		return
	}
	defer upstream.Close()

	errCh := make(chan error, 2)
	go proxyWebsocket(upstream, downstream, errCh)
	go proxyWebsocket(downstream, upstream, errCh)
	<-errCh
}

func proxyWebsocket(src, dst *websocket.Conn, errCh chan<- error) {
	for {
		messageType, message, err := src.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}
		if err := dst.WriteMessage(messageType, message); err != nil {
			errCh <- err
			return
		}
	}
}

func writeCommandResult(w http.ResponseWriter, result CommandResult) {
	payload := map[string]any{}
	if result.Stdout != "" {
		if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
			payload["raw_stdout"] = result.Stdout
		}
	}
	if result.Stderr != "" {
		payload["stderr"] = result.Stderr
	}
	payload["ok"] = result.ReturnCode == 0
	status := http.StatusOK
	if result.ReturnCode != 0 {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, payload)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if v := r.Header.Get("X-API-Key"); v != "" {
		return v
	}
	return r.URL.Query().Get("token")
}

func websocketProxyBaseURL(r *http.Request) string {
	scheme := "ws"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "wss"
	}
	host := r.Host
	return scheme + "://" + host + "/jarvis-browser/devtools"
}

func rewriteWebsocketURLs(payload any, proxyBase string) any {
	switch v := payload.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			if key == "webSocketDebuggerUrl" {
				if raw, ok := value.(string); ok {
					if parsed, err := url.Parse(raw); err == nil {
						out[key] = strings.TrimRight(proxyBase, "/") + "/" + strings.TrimLeft(parsed.Path, "/")
						continue
					}
				}
			}
			out[key] = rewriteWebsocketURLs(value, proxyBase)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = rewriteWebsocketURLs(item, proxyBase)
		}
		return out
	default:
		return payload
	}
}

func wsBaseURL(httpBase string) string {
	if strings.HasPrefix(httpBase, "https://") {
		return "wss://" + strings.TrimPrefix(httpBase, "https://")
	}
	return "ws://" + strings.TrimPrefix(httpBase, "http://")
}

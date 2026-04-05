package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	Token               string
	ChromeBaseURL       string
	BrowserBinary       string
	ProfileDir          string
	DownloadsDir        string
	StateDir            string
	DebugHost           string
	DebugPort           int
	Homepage            string
	Workspace           string
	StartupWait         time.Duration
	StopWait            time.Duration
	HealthCheckInterval time.Duration
	RestartCooldown     time.Duration
}

type CommandResult struct {
	ReturnCode int
	Stdout     string
	Stderr     string
}

type ProcessManager interface {
	Run(action string) CommandResult
}

type BrowserManager struct {
	cfg               Config
	client            *http.Client
	mu                sync.Mutex
	lastRestartAt     time.Time
	restartCount      int
	lastRestartReason string
}

type Server struct {
	cfg      Config
	manager  ProcessManager
	client   *http.Client
	upgrader websocket.Upgrader
}

func NewServer(cfg Config, manager ProcessManager) *Server {
	if cfg.ChromeBaseURL == "" {
		cfg.ChromeBaseURL = fmt.Sprintf("http://%s:%d", defaultString(cfg.DebugHost, "127.0.0.1"), defaultInt(cfg.DebugPort, 9222))
	}
	if cfg.DebugHost == "" {
		cfg.DebugHost = "127.0.0.1"
	}
	if cfg.DebugPort == 0 {
		cfg.DebugPort = 9222
	}
	if cfg.Homepage == "" {
		cfg.Homepage = "about:blank"
	}
	if cfg.StartupWait == 0 {
		cfg.StartupWait = 15 * time.Second
	}
	if cfg.StopWait == 0 {
		cfg.StopWait = 10 * time.Second
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = 30 * time.Second
	}
	if cfg.RestartCooldown == 0 {
		cfg.RestartCooldown = 15 * time.Second
	}
	if manager == nil {
		manager = &BrowserManager{
			cfg:    cfg,
			client: &http.Client{Timeout: 1 * time.Second},
		}
	}
	s := &Server{
		cfg:     cfg,
		manager: manager,
		client:  &http.Client{Timeout: 5 * time.Second},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	if browserManager, ok := manager.(*BrowserManager); ok && cfg.HealthCheckInterval > 0 {
		go browserManager.watchdog(cfg.HealthCheckInterval)
	}
	return s
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

	resp, err := s.getChromeJSON(endpoint)
	if err != nil && s.shouldAutoRecover(err) {
		if s.recoverBrowser("json proxy auto-recovery for /json/" + endpoint) {
			resp, err = s.getChromeJSON(endpoint)
		}
	}
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

func (s *Server) getChromeJSON(endpoint string) (*http.Response, error) {
	return s.client.Get(strings.TrimRight(s.cfg.ChromeBaseURL, "/") + "/json/" + endpoint)
}

func (s *Server) handleWebsocketProxy(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Token == "" || extractToken(r) != s.cfg.Token {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "unauthorized"})
		return
	}
	devtoolsPath := strings.TrimPrefix(r.URL.Path, "/jarvis-browser/devtools/")
	upstreamURL := wsBaseURL(s.cfg.ChromeBaseURL) + "/" + devtoolsPath

	upstream, err := s.dialDevtools(upstreamURL)
	if err != nil && s.shouldAutoRecover(err) {
		if s.recoverBrowser("websocket proxy auto-recovery") {
			upstream, err = s.dialDevtools(upstreamURL)
		}
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"detail": err.Error()})
		return
	}
	defer upstream.Close()

	downstream, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer downstream.Close()

	errCh := make(chan error, 2)
	go proxyWebsocket(upstream, downstream, errCh)
	go proxyWebsocket(downstream, upstream, errCh)
	<-errCh
}

func (m *BrowserManager) Run(action string) CommandResult {
	switch action {
	case "start":
		return m.start()
	case "stop":
		return m.stop()
	case "restart":
		return m.restart("api request")
	case "status":
		return m.statusResult(0, "")
	default:
		return CommandResult{ReturnCode: 1, Stderr: "unsupported action: " + action}
	}
}

func (m *BrowserManager) start() CommandResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startLocked()
}

func (m *BrowserManager) startLocked() CommandResult {
	running, pid := m.browserRunningLocked()
	if running {
		if m.cdpReady() {
			return m.statusResultLocked(0, "")
		}
		return m.restartLocked("start requested while CDP unhealthy")
	}

	if err := m.ensureDirs(); err != nil {
		return CommandResult{ReturnCode: 1, Stderr: err.Error()}
	}

	browser, err := m.findBrowser()
	if err != nil {
		return CommandResult{ReturnCode: 1, Stderr: err.Error()}
	}

	cmd := exec.Command(browser, m.browserArgs()...)
	nullFile, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		defer nullFile.Close()
		cmd.Stdout = nullFile
		cmd.Stderr = nullFile
	}

	if err := cmd.Start(); err != nil {
		return CommandResult{ReturnCode: 1, Stderr: err.Error()}
	}
	pid = cmd.Process.Pid
	if err := os.WriteFile(m.pidFile(), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		_ = cmd.Process.Kill()
		return CommandResult{ReturnCode: 1, Stderr: err.Error()}
	}

	go func() {
		_ = cmd.Wait()
		m.removePIDIfMatches(pid)
	}()
	go m.moveToWorkspace()

	if m.cfg.StartupWait > 0 {
		deadline := time.Now().Add(m.cfg.StartupWait)
		for time.Now().Before(deadline) {
			if m.cdpReady() {
				break
			}
			if !processAlive(pid) {
				return CommandResult{ReturnCode: 1, Stderr: "browser exited before DevTools became ready", Stdout: m.mustStatusJSON()}
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	return m.statusResultLocked(0, "")
}

func (m *BrowserManager) stop() CommandResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.stopLocked()
}

func (m *BrowserManager) stopLocked() CommandResult {
	pid, err := m.readPID()
	if err != nil || pid <= 0 || !processAlive(pid) {
		m.removePIDIfMatches(pid)
		return m.statusResultLocked(0, "")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return CommandResult{ReturnCode: 1, Stderr: err.Error()}
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && !strings.Contains(err.Error(), "finished") {
		return CommandResult{ReturnCode: 1, Stderr: err.Error()}
	}

	deadline := time.Now().Add(m.cfg.StopWait)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			m.removePIDIfMatches(pid)
			return m.statusResultLocked(0, "")
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := proc.Signal(syscall.SIGKILL); err != nil && !strings.Contains(err.Error(), "finished") {
		return CommandResult{ReturnCode: 1, Stderr: err.Error()}
	}

	for i := 0; i < 20; i++ {
		if !processAlive(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	m.removePIDIfMatches(pid)
	return m.statusResultLocked(0, "")
}

func (m *BrowserManager) restart(reason string) CommandResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restartLocked(reason)
}

func (m *BrowserManager) restartLocked(reason string) CommandResult {
	stopResult := m.stopLocked()
	if stopResult.ReturnCode != 0 {
		return stopResult
	}
	m.lastRestartAt = time.Now().UTC()
	m.restartCount++
	m.lastRestartReason = reason
	return m.startLocked()
}

func (m *BrowserManager) statusResult(code int, stderr string) CommandResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusResultLocked(code, stderr)
}

func (m *BrowserManager) statusResultLocked(code int, stderr string) CommandResult {
	cdpReady := m.cdpReady()
	payload := map[string]any{
		"browser_running": false,
		"cdp_ready":       cdpReady,
		"healthy":         false,
		"debug_url":       strings.TrimRight(m.cfg.ChromeBaseURL, "/") + "/json/version",
		"workspace":       m.cfg.Workspace,
		"restart_count":   m.restartCount,
	}
	if !m.lastRestartAt.IsZero() {
		payload["last_restart_at"] = m.lastRestartAt.Format(time.RFC3339)
	}
	if m.lastRestartReason != "" {
		payload["last_restart_reason"] = m.lastRestartReason
	}
	if pid, err := m.readPID(); err == nil && pid > 0 && processAlive(pid) {
		payload["browser_running"] = true
		payload["browser_pid"] = pid
		payload["healthy"] = cdpReady
	} else if pid > 0 {
		m.removePIDIfMatches(pid)
	}
	stdout, _ := json.Marshal(payload)
	return CommandResult{ReturnCode: code, Stdout: string(stdout), Stderr: stderr}
}

func (m *BrowserManager) mustStatusJSON() string {
	return m.statusResult(0, "").Stdout
}

func (m *BrowserManager) ensureDirs() error {
	for _, dir := range []string{m.cfg.ProfileDir, m.cfg.DownloadsDir, m.cfg.StateDir} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (m *BrowserManager) findBrowser() (string, error) {
	if m.cfg.BrowserBinary != "" {
		path, err := exec.LookPath(m.cfg.BrowserBinary)
		if err != nil {
			return "", fmt.Errorf("browser not found: %s", m.cfg.BrowserBinary)
		}
		return path, nil
	}
	for _, candidate := range []string{"chromium", "google-chrome-stable", "google-chrome", "chromium-browser"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no supported browser found; install chromium or chrome")
}

func (m *BrowserManager) browserArgs() []string {
	return []string{
		"--ozone-platform=wayland",
		"--enable-features=UseOzonePlatform",
		"--user-data-dir=" + m.cfg.ProfileDir,
		"--remote-debugging-address=" + m.cfg.DebugHost,
		"--remote-debugging-port=" + strconv.Itoa(m.cfg.DebugPort),
		"--class=JarvisChrome",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-features=MediaRouter",
		"--homepage=" + m.cfg.Homepage,
		m.cfg.Homepage,
	}
}

func (m *BrowserManager) moveToWorkspace() {
	if strings.TrimSpace(m.cfg.Workspace) == "" {
		return
	}
	if _, err := exec.LookPath("hyprctl"); err != nil {
		return
	}
	selector := m.cfg.Workspace + ",class:^(JarvisChrome)$"
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = exec.Command("hyprctl", "dispatch", "movetoworkspacesilent", selector).Run()
		time.Sleep(250 * time.Millisecond)
	}
}

func (m *BrowserManager) cdpReady() bool {
	resp, err := m.client.Get(strings.TrimRight(m.cfg.ChromeBaseURL, "/") + "/json/version")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 400
}

func (m *BrowserManager) watchdog(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		m.ensureHealthy("watchdog detected unhealthy CDP")
	}
}

func (m *BrowserManager) ensureHealthy(reason string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	running, _ := m.browserRunningLocked()
	if !running || m.cdpReady() {
		return false
	}
	if m.cfg.RestartCooldown > 0 && !m.lastRestartAt.IsZero() && time.Since(m.lastRestartAt) < m.cfg.RestartCooldown {
		return false
	}

	result := m.restartLocked(reason)
	return result.ReturnCode == 0
}

func (m *BrowserManager) pidFile() string {
	return filepath.Join(m.cfg.StateDir, "browser.pid")
}

func (m *BrowserManager) readPID() (int, error) {
	data, err := os.ReadFile(m.pidFile())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func (m *BrowserManager) removePIDIfMatches(pid int) {
	current, err := m.readPID()
	if err != nil {
		return
	}
	if pid == 0 || current == pid {
		_ = os.Remove(m.pidFile())
	}
}

func (m *BrowserManager) browserRunningLocked() (bool, int) {
	pid, err := m.readPID()
	if err != nil || pid <= 0 {
		return false, 0
	}
	if !processAlive(pid) {
		_ = os.Remove(m.pidFile())
		return false, 0
	}
	return true, pid
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func (s *Server) dialDevtools(upstreamURL string) (*websocket.Conn, error) {
	upstream, _, err := websocket.DefaultDialer.Dial(upstreamURL, nil)
	return upstream, err
}

func (s *Server) recoverBrowser(reason string) bool {
	if browserManager, ok := s.manager.(*BrowserManager); ok {
		return browserManager.restart(reason).ReturnCode == 0
	}
	return s.manager.Run("restart").ReturnCode == 0
}

func (s *Server) shouldAutoRecover(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connect: connection refused") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "i/o timeout")
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

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

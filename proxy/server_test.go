package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

type fakeManager struct {
	results map[string]CommandResult
}

func (f fakeManager) Run(action string) CommandResult {
	return f.results[action]
}

func newTestServer(t *testing.T, manager ProcessManager, chromeBase string) *httptest.Server {
	t.Helper()
	srv := NewServer(Config{
		Token:         "secret-token",
		BrowserCmd:    "jarvis-browser",
		ChromeBaseURL: chromeBase,
	}, manager)
	return httptest.NewServer(srv.Routes())
}

func authRequest(t *testing.T, method, rawURL string, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	return req
}

func TestStatusRequiresAuth(t *testing.T) {
	ts := newTestServer(t, fakeManager{results: map[string]CommandResult{"status": {ReturnCode: 0, Stdout: `{}`}}}, "http://127.0.0.1:9222")
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/jarvis-browser/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestStatusReturnsCommandJSON(t *testing.T) {
	ts := newTestServer(t, fakeManager{results: map[string]CommandResult{"status": {ReturnCode: 0, Stdout: `{"running":true}`}}}, "http://127.0.0.1:9222")
	defer ts.Close()

	resp, err := http.DefaultClient.Do(authRequest(t, http.MethodGet, ts.URL+"/jarvis-browser/status", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if payload["running"] != true || payload["ok"] != true {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestStartSurfacesStderrOnFailure(t *testing.T) {
	ts := newTestServer(t, fakeManager{results: map[string]CommandResult{"start": {ReturnCode: 1, Stdout: `{"service_active":false}`, Stderr: "boom"}}}, "http://127.0.0.1:9222")
	defer ts.Close()

	resp, err := http.DefaultClient.Do(authRequest(t, http.MethodPost, ts.URL+"/jarvis-browser/start", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
	if payload["stderr"] != "boom" || payload["ok"] != false {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestJSONVersionRewritesWebsocketDebuggerURL(t *testing.T) {
	chrome := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Browser":              "Chromium/123",
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/abc123",
		})
	}))
	defer chrome.Close()

	ts := newTestServer(t, fakeManager{results: map[string]CommandResult{}}, chrome.URL)
	defer ts.Close()

	resp, err := http.DefaultClient.Do(authRequest(t, http.MethodGet, ts.URL+"/jarvis-browser/json/version", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	expected := strings.Replace(ts.URL, "http://", "ws://", 1) + "/jarvis-browser/devtools/devtools/browser/abc123"
	if payload["webSocketDebuggerUrl"] != expected {
		t.Fatalf("expected %s, got %#v", expected, payload["webSocketDebuggerUrl"])
	}
}

func TestJSONListRewritesNestedItems(t *testing.T) {
	chrome := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id":                   "page-1",
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/page/page-1",
		}})
	}))
	defer chrome.Close()

	ts := newTestServer(t, fakeManager{results: map[string]CommandResult{}}, chrome.URL)
	defer ts.Close()

	resp, err := http.DefaultClient.Do(authRequest(t, http.MethodGet, ts.URL+"/jarvis-browser/json/list", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var payload []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	expected := strings.Replace(ts.URL, "http://", "ws://", 1) + "/jarvis-browser/devtools/devtools/page/page-1"
	if payload[0]["webSocketDebuggerUrl"] != expected {
		t.Fatalf("expected %s, got %#v", expected, payload[0]["webSocketDebuggerUrl"])
	}
}

func TestWebsocketProxy(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	chrome := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if err := conn.WriteMessage(mt, append([]byte("upstream:"), msg...)); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}))
	defer chrome.Close()

	chromeWS := "http://" + strings.TrimPrefix(chrome.URL, "http://")
	ts := newTestServer(t, fakeManager{results: map[string]CommandResult{}}, chromeWS)
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/jarvis-browser/devtools/devtools/page/test?token=secret-token"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "upstream:hello" {
		t.Fatalf("unexpected websocket reply: %s", msg)
	}
}

func TestRewriteUsesForwardedProtoForWSS(t *testing.T) {
	chrome := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/abc123",
		})
	}))
	defer chrome.Close()

	srv := NewServer(Config{Token: "secret-token", ChromeBaseURL: chrome.URL}, fakeManager{results: map[string]CommandResult{}})
	rec := httptest.NewRecorder()
	req := authRequest(t, http.MethodGet, "http://example.test/jarvis-browser/json/version", "")
	req.Host = "example.test"
	req.Header.Set("X-Forwarded-Proto", "https")

	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if got := payload["webSocketDebuggerUrl"]; got != "wss://example.test/jarvis-browser/devtools/devtools/browser/abc123" {
		t.Fatalf("unexpected websocket url: %#v", got)
	}
}

func TestUnauthorizedWebsocketRejected(t *testing.T) {
	ts := newTestServer(t, fakeManager{results: map[string]CommandResult{}}, "http://127.0.0.1:9222")
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/jarvis-browser/devtools/devtools/page/test"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected websocket dial to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 response, got %#v", resp)
	}
}

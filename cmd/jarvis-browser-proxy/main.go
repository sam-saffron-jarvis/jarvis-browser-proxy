package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/samsaffron/jarvis-browser-proxy/proxy"
)

func main() {
	addr := flag.String("addr", getenvDefault("JARVIS_BROWSER_PROXY_ADDR", "0.0.0.0:8787"), "listen address")
	token := flag.String("token", getenvDefault("JARVIS_BROWSER_PROXY_TOKEN", ""), "shared API token")
	chromeBaseURL := flag.String("chrome-base-url", getenvDefault("JARVIS_CHROME_BASE_URL", "http://127.0.0.1:9222"), "upstream chrome devtools base URL")
	browserBinary := flag.String("browser-binary", getenvDefault("JARVIS_CHROME_BROWSER", ""), "browser binary to launch")
	profileDir := flag.String("profile-dir", getenvDefault("JARVIS_CHROME_PROFILE", defaultPath("HOME", ".local/share/jarvis-chrome")), "browser profile directory")
	downloadsDir := flag.String("downloads-dir", getenvDefault("JARVIS_CHROME_DOWNLOADS", defaultPath("HOME", ".local/share/jarvis-chrome/downloads")), "browser downloads directory")
	stateDir := flag.String("state-dir", getenvDefault("JARVIS_CHROME_STATE_DIR", defaultPath("HOME", ".local/state/jarvis-browser-proxy")), "state directory")
	debugHost := flag.String("debug-host", getenvDefault("JARVIS_CHROME_DEBUG_HOST", "127.0.0.1"), "chrome remote debugging host")
	debugPort := flag.Int("debug-port", getenvIntDefault("JARVIS_CHROME_DEBUG_PORT", 9222), "chrome remote debugging port")
	homepage := flag.String("homepage", getenvDefault("JARVIS_CHROME_HOMEPAGE", "about:blank"), "browser homepage")
	workspace := flag.String("workspace", getenvDefault("JARVIS_CHROME_WORKSPACE", "9"), "Hyprland workspace to move the browser to; empty disables the move")
	startupWait := flag.Duration("startup-wait", getenvDurationDefault("JARVIS_CHROME_STARTUP_WAIT", 15*time.Second), "how long to wait for CDP after launch")
	stopWait := flag.Duration("stop-wait", getenvDurationDefault("JARVIS_CHROME_STOP_WAIT", 10*time.Second), "how long to wait before force-killing the browser")
	flag.Parse()

	if *token == "" {
		log.Fatal("token is required via -token or JARVIS_BROWSER_PROXY_TOKEN")
	}

	cfg := proxy.Config{
		Token:         *token,
		ChromeBaseURL: *chromeBaseURL,
		BrowserBinary: *browserBinary,
		ProfileDir:    *profileDir,
		DownloadsDir:  *downloadsDir,
		StateDir:      *stateDir,
		DebugHost:     *debugHost,
		DebugPort:     *debugPort,
		Homepage:      *homepage,
		Workspace:     *workspace,
		StartupWait:   *startupWait,
		StopWait:      *stopWait,
	}

	server := proxy.NewServer(cfg, nil)
	log.Printf("jarvis-browser-proxy listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, server.Routes()))
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvIntDefault(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			log.Fatalf("invalid integer for %s: %v", key, err)
		}
		return parsed
	}
	return fallback
}

func getenvDurationDefault(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			log.Fatalf("invalid duration for %s: %v", key, err)
		}
		return parsed
	}
	return fallback
}

func defaultPath(envKey, suffix string) string {
	base := os.Getenv(envKey)
	if base == "" {
		return suffix
	}
	return base + "/" + suffix
}

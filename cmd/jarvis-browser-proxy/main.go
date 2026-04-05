package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/samsaffron/jarvis-browser-proxy/proxy"
)

func main() {
	addr := flag.String("addr", getenvDefault("JARVIS_BROWSER_PROXY_ADDR", "127.0.0.1:8787"), "listen address")
	token := flag.String("token", getenvDefault("JARVIS_BROWSER_PROXY_TOKEN", ""), "shared API token")
	browserCmd := flag.String("browser-command", getenvDefault("JARVIS_BROWSER_COMMAND", "jarvis-browser"), "browser control command")
	chromeBaseURL := flag.String("chrome-base-url", getenvDefault("JARVIS_CHROME_BASE_URL", "http://127.0.0.1:9222"), "upstream chrome devtools base URL")
	flag.Parse()

	if *token == "" {
		log.Fatal("token is required via -token or JARVIS_BROWSER_PROXY_TOKEN")
	}

	cfg := proxy.Config{
		Token:         *token,
		BrowserCmd:    *browserCmd,
		ChromeBaseURL: *chromeBaseURL,
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

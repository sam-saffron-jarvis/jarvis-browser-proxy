package main

import (
	"log"
	"net/http"
	"os"

	"github.com/samsaffron/jarvis-browser-proxy/proxy"
)

func main() {
	token := os.Getenv("JARVIS_BROWSER_PROXY_TOKEN")
	if token == "" {
		log.Fatal("JARVIS_BROWSER_PROXY_TOKEN is required")
	}

	cfg := proxy.Config{
		Token:         token,
		BrowserCmd:    getenvDefault("JARVIS_BROWSER_COMMAND", "jarvis-browser"),
		ChromeBaseURL: getenvDefault("JARVIS_CHROME_BASE_URL", "http://127.0.0.1:9222"),
	}

	addr := getenvDefault("JARVIS_BROWSER_PROXY_ADDR", "127.0.0.1:8787")
	server := proxy.NewServer(cfg, nil)
	log.Printf("jarvis-browser-proxy listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.Routes()))
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/viper"
)

// healthcheckTimeout is a variable so tests can shorten it.
var healthcheckTimeout = 5 * time.Second

// healthcheckURL builds http://127.0.0.1:<port>/robots.txt from http_addr.
func healthcheckURL(httpAddr string) (string, error) {
	_, port, err := net.SplitHostPort(httpAddr)
	if err != nil {
		return "", fmt.Errorf("invalid http_addr %q: %w", httpAddr, err)
	}
	if port == "" {
		return "", fmt.Errorf("invalid http_addr %q: empty port", httpAddr)
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port) + "/robots.txt", nil
}

// checkHealth GETs url with healthcheckTimeout; nil only for status 200.
func checkHealth(url string) error {
	client := &http.Client{Timeout: healthcheckTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// healthcheckMain probes the local server and exits 0 (healthy) or 1.
func healthcheckMain() {
	url, err := healthcheckURL(viper.GetString("http_addr"))
	if err == nil {
		err = checkHealth(url)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %s\n", err)
		os.Exit(1)
	}
}

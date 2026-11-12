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

// checkHealth GETs url with healthcheckTimeout; nil for status 200 or a
// redirect. Redirects are not followed: with http_redirect the target is
// https:// on the plain HTTP port, which can never answer.
func checkHealth(url string) error {
	client := &http.Client{
		Timeout: healthcheckTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	isRedirect := resp.StatusCode >= 300 && resp.StatusCode < 400
	if resp.StatusCode != http.StatusOK && !isRedirect {
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

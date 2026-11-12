package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthcheckURL(t *testing.T) {
	cases := map[string]string{
		":8080":          "http://127.0.0.1:8080/robots.txt",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/robots.txt",
		"127.0.0.1:8080": "http://127.0.0.1:8080/robots.txt",
	}
	for in, want := range cases {
		got, err := healthcheckURL(in)
		if err != nil || got != want {
			t.Errorf("healthcheckURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "nonsense"} {
		if got, err := healthcheckURL(in); err == nil {
			t.Errorf("healthcheckURL(%q) = %q, want error", in, got)
		}
	}
}

func TestCheckHealthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	if err := checkHealth(srv.URL); err != nil {
		t.Fatalf("checkHealth: %v", err)
	}
}

func TestCheckHealthNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	err := checkHealth(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("checkHealth err = %v, want error with 503", err)
	}
}

func TestCheckHealthTimeout(t *testing.T) {
	old := healthcheckTimeout
	healthcheckTimeout = 100 * time.Millisecond
	t.Cleanup(func() { healthcheckTimeout = old })

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	start := time.Now()
	if err := checkHealth(srv.URL); err == nil {
		t.Fatal("checkHealth: want timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("checkHealth took %s, timeout not applied", time.Since(start))
	}
}

func TestCheckHealthUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	if err := checkHealth("http://" + addr + "/robots.txt"); err == nil {
		t.Fatal("checkHealth: want connection error")
	}
}

// TestCheckHealthRedirectIsHealthy pins that with http_redirect=true the probe
// takes the 307 as healthy instead of following it to https:// on an HTTP port.
func TestCheckHealthRedirectIsHealthy(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "https://"+r.Host+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	if err := checkHealth(srv.URL + "/robots.txt"); err != nil {
		t.Fatalf("checkHealth: %v, want nil for a 307", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("requests = %d, want 1 (redirect not followed)", n)
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStaticdumpClientRejectsUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL)
	}))
	t.Cleanup(srv.Close)

	client := newStaticdumpHTTPClient()
	client.Backoff = func(int) time.Duration { return 0 }
	resp, err := client.Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("request to a self-signed server succeeded, want certificate error")
	}
	if !strings.Contains(err.Error(), "x509") {
		t.Errorf("error = %v, want a x509 verification error", err)
	}
}

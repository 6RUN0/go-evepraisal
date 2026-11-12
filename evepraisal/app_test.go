package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/evepraisal/go-evepraisal/bolt"
)

// TestPrepareDataDirsFreshDataDir pins that the service starts on an empty
// bind-mounted data directory (README: mkdir data), where db/ does not exist.
func TestPrepareDataDirsFreshDataDir(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(data, "db") + "/"
	backupPath := filepath.Join(data, "db", "backups") + "/"

	for range 2 {
		if err := prepareDataDirs(dbPath, backupPath); err != nil {
			t.Fatalf("prepareDataDirs: %v", err)
		}
	}
	for _, dir := range []string{dbPath, backupPath} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("%s: %v, want a directory", dir, err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("%s mode = %o, want 700", dir, perm)
		}
	}
	priceDB, err := bolt.NewPriceDB(filepath.Join(dbPath, "prices"))
	if err != nil {
		t.Fatalf("NewPriceDB in a prepared dir: %v", err)
	}
	if err := priceDB.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestShutdownHTTPServerWaitsForRequests pins that the shutdown budget starts
// at the stop signal, so an in-flight request completes instead of being cut.
func TestShutdownHTTPServerWaitsForRequests(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, "done")
	}))
	defer srv.Close()

	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get(srv.URL)
		if err != nil {
			got <- result{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		got <- result{body: string(body), err: err}
	}()
	<-started

	shutdownHTTPServer(srv.Config, 5*time.Second)

	r := <-got
	if r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request = %q, %v; want done", r.body, r.err)
	}
}

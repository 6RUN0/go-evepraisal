package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/evepraisal/go-evepraisal"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// TestRestoreSaverCountsConflictAsSkipped pins 409 as an already stored
// appraisal: it is skipped, neither logged nor counted as a failure.
func TestRestoreSaverCountsConflictAsSkipped(t *testing.T) {
	logs := captureLog(t)
	statuses := []int{http.StatusConflict, http.StatusOK, http.StatusConflict}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/restore" {
			t.Errorf("request = %s %s, want POST /restore", r.Method, r.URL.Path)
		}
		w.WriteHeader(statuses[calls])
		calls++
	}))
	defer server.Close()

	saver := newRestoreSaver(server.Client(), server.URL)
	for range statuses {
		if err := saver.save(&evepraisal.Appraisal{ID: "a", Kind: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if saver.counts != (restoreCounts{skipped: 2}) {
		t.Fatalf("counts = %+v, want skipped=2 failed=0", saver.counts)
	}
	if strings.Contains(logs.String(), "ERROR") {
		t.Fatalf("log = %q, want no ERROR", logs.String())
	}
}

// TestRestoreSaverCountsNon2xxAsFailed pins a refused row: it must be counted
// so restore exits non-zero, and its status and body must reach the log.
func TestRestoreSaverCountsNon2xxAsFailed(t *testing.T) {
	logs := captureLog(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	saver := newRestoreSaver(server.Client(), server.URL)
	if err := saver.save(&evepraisal.Appraisal{ID: "a", Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	if saver.counts != (restoreCounts{failed: 1}) {
		t.Fatalf("counts = %+v, want skipped=0 failed=1", saver.counts)
	}
	if got := logs.String(); !strings.Contains(got, "500") || !strings.Contains(got, "boom") {
		t.Fatalf("log = %q, want status 500 and body boom", got)
	}
}

// TestRestoreSaverCountsTransportError pins an unreachable management API as
// a failure instead of a silently lost row.
func TestRestoreSaverCountsTransportError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	saver := newRestoreSaver(http.DefaultClient, url)
	if err := saver.save(&evepraisal.Appraisal{ID: "a", Kind: "test"}); err == nil {
		t.Fatal("save() = nil, want a transport error")
	}
	if saver.counts != (restoreCounts{failed: 1}) {
		t.Fatalf("counts = %+v, want skipped=0 failed=1", saver.counts)
	}
}

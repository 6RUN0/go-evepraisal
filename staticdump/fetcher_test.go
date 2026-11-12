package staticdump

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evepraisal/go-evepraisal/typedb"
)

const (
	testBuild       = "200"
	testArchivePath = "/static-data/tranquility/eve-online-static-data-200-jsonl.zip"
	sdeLabel        = "sde"
)

// sdeServer serves a manifest for build 200 and delegates the archive to archive.
// It counts archive requests.
func sdeServer(t *testing.T, manifestStatus int, archive http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var archiveRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case manifestPath:
			w.WriteHeader(manifestStatus)
			_, _ = w.Write([]byte(`{"_key": "sde", "buildNumber": ` + testBuild + `, "releaseDate": "2026-11-11T11:07:08Z"}` + "\n"))
		case testArchivePath:
			archiveRequests.Add(1)
			archive(w, r)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &archiveRequests
}

func serveFile(t *testing.T, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

func unexpectedArchive(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected archive request %s", r.URL)
		http.NotFound(w, r)
	}
}

// dbRecorder is a fetcher callback that, like the app, closes the previous DB.
// It records which database each call delivered: the fixture name or "sde".
type dbRecorder struct {
	mu        sync.Mutex
	labels    []string
	current   typedb.TypeDB
	delivered chan struct{}
}

func newDBRecorder(t *testing.T) *dbRecorder {
	r := &dbRecorder{delivered: make(chan struct{}, 16)}
	t.Cleanup(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.current != nil {
			_ = r.current.Close()
		}
	})
	return r
}

func (r *dbRecorder) callback(db typedb.TypeDB) {
	label := "unknown"
	if ty, ok := db.GetTypeByID(fixtureTypeID); ok {
		label = ty.Name
	} else if ty, ok := db.GetTypeByID(24698); ok && ty.PackagedVolume == 15000 {
		label = sdeLabel
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		_ = r.current.Close()
	}
	r.current = db
	r.labels = append(r.labels, label)
	r.delivered <- struct{}{}
}

func (r *dbRecorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.labels...)
}

func assertCalls(t *testing.T, r *dbRecorder, want ...string) {
	t.Helper()
	got := r.calls()
	if len(got) != len(want) {
		t.Fatalf("callback calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("callback calls = %v, want %v", got, want)
		}
	}
}

func newTestFetcher(srv *httptest.Server, dir string, rec *dbRecorder) *StaticFetcher {
	return newStaticFetcher(newTestClient(), dir, srv.URL+manifestPath, true, rec.callback)
}

func TestRunOnceLoadsNewBuild(t *testing.T) {
	dir := t.TempDir()
	makeReadyTypeDB(t, dir, "types-100")
	makeReadyTypeDB(t, dir, "types-deadbeef")
	srv, _ := sdeServer(t, http.StatusOK, serveFile(t, sampleZip(t)))
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)

	if err := f.RunOnce(); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	assertCalls(t, rec, sdeLabel)
	newDB := filepath.Join(dir, "types-"+testBuild)
	assertExists(t, newDB, newDB+".index", newDB+".zip")
	assertMissing(t,
		filepath.Join(dir, "types-100"), filepath.Join(dir, "types-100.index"),
		filepath.Join(dir, "types-deadbeef"), filepath.Join(dir, "types-deadbeef.index"))
	assertNoGlob(t, filepath.Join(dir, "*.partial*"))
	assertNoGlob(t, filepath.Join(dir, "*.download"))
}

func TestRunOnceSkipsBuildLoadedEarlier(t *testing.T) {
	dir := t.TempDir()
	srv, archiveRequests := sdeServer(t, http.StatusOK, serveFile(t, sampleZip(t)))
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)
	if err := f.RunOnce(); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if archiveRequests.Load() != 1 {
		t.Fatalf("archive requests = %d, want 1", archiveRequests.Load())
	}

	if err := f.RunOnce(); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if archiveRequests.Load() != 1 {
		t.Errorf("archive requested again: %d requests", archiveRequests.Load())
	}
	assertCalls(t, rec, sdeLabel)
}

func TestRunOnceReusesExistingBuild(t *testing.T) {
	dir := t.TempDir()
	makeReadyTypeDB(t, dir, "types-"+testBuild)
	srv, _ := sdeServer(t, http.StatusOK, unexpectedArchive(t))
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)
	f.currentPath = filepath.Join(dir, "types-"+testBuild)

	if err := f.RunOnce(); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	assertCalls(t, rec)
}

func TestRunOnceFallsBackWhenManifestDown(t *testing.T) {
	dir := t.TempDir()
	old := makeReadyTypeDB(t, dir, "types-100")
	srv, _ := sdeServer(t, http.StatusServiceUnavailable, unexpectedArchive(t))
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)
	f.currentPath = old

	if err := f.RunOnce(); err == nil {
		t.Fatal("RunOnce: want error when the manifest is down")
	}
	assertCalls(t, rec)
	assertExists(t, old, old+".index")
	if f.currentPath != old {
		t.Errorf("currentPath = %s, want %s", f.currentPath, old)
	}
}

func corruptArchive(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("0123456789"))
}

func assertNoBuildLeftovers(t *testing.T, dir string) {
	t.Helper()
	newDB := filepath.Join(dir, "types-"+testBuild)
	assertMissing(t, newDB, newDB+".index", newDB+".zip")
	assertNoGlob(t, filepath.Join(dir, "*.download"))
	assertNoGlob(t, filepath.Join(dir, "*.partial*"))
}

func TestRunOnceRemovesCorruptArchive(t *testing.T) {
	dir := t.TempDir()
	old := makeReadyTypeDB(t, dir, "types-100")
	srv, _ := sdeServer(t, http.StatusOK, corruptArchive)
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)
	f.currentPath = old

	if err := f.RunOnce(); err == nil {
		t.Fatal("RunOnce: want error for a corrupt archive")
	}
	assertCalls(t, rec)
	assertNoBuildLeftovers(t, dir)
	assertExists(t, old, old+".index")
	if f.currentPath != old {
		t.Errorf("currentPath = %s, want %s", f.currentPath, old)
	}
}

func TestRunOnceRecoversAfterCorruptArchive(t *testing.T) {
	dir := t.TempDir()
	old := makeReadyTypeDB(t, dir, "types-100")
	valid := sampleZip(t)
	var calls atomic.Int32
	srv, _ := sdeServer(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			corruptArchive(w, r)
			return
		}
		http.ServeFile(w, r, valid)
	})
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)
	f.currentPath = old

	if err := f.RunOnce(); err == nil {
		t.Fatal("first RunOnce: want error for a corrupt archive")
	}
	if err := f.RunOnce(); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	assertCalls(t, rec, sdeLabel)
	newDB := filepath.Join(dir, "types-"+testBuild)
	assertExists(t, newDB, newDB+".index", newDB+".zip")
	assertMissing(t, old, old+".index")
}

func TestRunOnceTruncatedDownload(t *testing.T) {
	dir := t.TempDir()
	srv, _ := sdeServer(t, http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("response writer is not a Hijacker")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/zip\r\nContent-Length: 5000\r\n\r\n")
		_, _ = buf.Write(make([]byte, 100))
		_ = buf.Flush()
	})
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)

	if err := f.RunOnce(); err == nil {
		t.Fatal("RunOnce: want error for a truncated download")
	}
	assertCalls(t, rec)
	assertNoBuildLeftovers(t, dir)
}

func TestRunOnceNon2xxArchive(t *testing.T) {
	dir := t.TempDir()
	srv, _ := sdeServer(t, http.StatusOK, http.NotFound)
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)

	if err := f.RunOnce(); err == nil {
		t.Fatal("RunOnce: want error for a 404 archive")
	}
	assertCalls(t, rec)
	assertNoBuildLeftovers(t, dir)
}

func TestNewStaticFetcherServesOldDBImmediately(t *testing.T) {
	dir := t.TempDir()
	makeReadyTypeDB(t, dir, "types-100")
	valid := sampleZip(t)
	release := make(chan struct{})
	srv, _ := sdeServer(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		<-release
		http.ServeFile(w, r, valid)
	})
	rec := newDBRecorder(t)

	f := newTestFetcher(srv, dir, rec).start()
	assertCalls(t, rec, "types-100")
	<-rec.delivered

	close(release)
	select {
	case <-rec.delivered:
	case <-time.After(30 * time.Second):
		t.Fatal("background build did not deliver the new DB")
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertCalls(t, rec, "types-100", sdeLabel)
	assertMissing(t, filepath.Join(dir, "types-100"))
}

func TestNewStaticFetcherRemovesPartialOnStart(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "types-"+testBuild+".partial")
	touchFile(t, partial)
	touchDir(t, partial+".index")
	srv, _ := sdeServer(t, http.StatusServiceUnavailable, unexpectedArchive(t))
	rec := newDBRecorder(t)

	f := newTestFetcher(srv, dir, rec).start()
	assertMissing(t, partial, partial+".index")
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertCalls(t, rec)
}

package staticdump

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

// pauseRecorder replaces StaticFetcher.after: it records every pause and fires
// at once until n pauses are recorded, then never fires and closes done.
type pauseRecorder struct {
	mu     sync.Mutex
	pauses []time.Duration
	n      int
	done   chan struct{}
}

func newPauseRecorder(n int) *pauseRecorder {
	return &pauseRecorder{n: n, done: make(chan struct{})}
}

func (r *pauseRecorder) after(d time.Duration) <-chan time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pauses = append(r.pauses, d)
	if len(r.pauses) == r.n {
		close(r.done)
		return nil
	}
	fired := make(chan time.Time, 1)
	fired <- time.Time{}
	return fired
}

// runPauses starts f with rec as its timer and returns the recorded pauses.
func runPauses(t *testing.T, f *StaticFetcher, rec *pauseRecorder) []time.Duration {
	t.Helper()
	f.after = rec.after
	f.start()
	select {
	case <-rec.done:
	case <-time.After(30 * time.Second):
		t.Fatal("fetcher did not reach the expected number of pauses")
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]time.Duration(nil), rec.pauses...)
}

func assertPauses(t *testing.T, got []time.Duration, want ...time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("pauses = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pauses = %v, want %v", got, want)
		}
	}
}

// TestStaticFetcherRetriesFirstBuildWithBackoff pins that a failed first build
// on an empty db_path is retried after 5 minutes, doubling up to 6 hours,
// instead of leaving the site without a type DB for 6 hours.
func TestStaticFetcherRetriesFirstBuildWithBackoff(t *testing.T) {
	srv, _ := sdeServer(t, http.StatusServiceUnavailable, unexpectedArchive(t))
	f := newTestFetcher(srv, t.TempDir(), newDBRecorder(t))

	got := runPauses(t, f, newPauseRecorder(9))
	assertPauses(t, got,
		5*time.Minute, 10*time.Minute, 20*time.Minute, 40*time.Minute, 80*time.Minute,
		160*time.Minute, 320*time.Minute, 6*time.Hour, 6*time.Hour)
}

// TestStaticFetcherRefreshesEverySixHoursOnceServing pins the regular refresh
// interval while a type DB is served, even when the refresh itself fails.
func TestStaticFetcherRefreshesEverySixHoursOnceServing(t *testing.T) {
	dir := t.TempDir()
	makeReadyTypeDB(t, dir, "types-100")
	srv, _ := sdeServer(t, http.StatusServiceUnavailable, unexpectedArchive(t))
	f := newTestFetcher(srv, dir, newDBRecorder(t))

	got := runPauses(t, f, newPauseRecorder(3))
	assertPauses(t, got, 6*time.Hour, 6*time.Hour, 6*time.Hour)
}

// TestOpenNewestTypeDBKeepsPartialBuilds pins that restore opens the ready DB
// without touching the .partial build of a running service.
func TestOpenNewestTypeDBKeepsPartialBuilds(t *testing.T) {
	dir := t.TempDir()
	makeReadyTypeDB(t, dir, "types-100")
	partial := filepath.Join(dir, "types-"+testBuild+".partial")
	touchFile(t, partial)
	touchDir(t, partial+".index")

	db, err := OpenNewestTypeDB(dir)
	if err != nil {
		t.Fatalf("OpenNewestTypeDB: %v", err)
	}
	defer db.Close()
	if ty, ok := db.GetTypeByID(fixtureTypeID); !ok || ty.Name != "types-100" {
		t.Errorf("opened DB type = %v %v, want types-100", ty.Name, ok)
	}
	assertExists(t, partial, partial+".index")
}

func TestOpenNewestTypeDBNone(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "types-"+testBuild+".partial")
	touchFile(t, partial)

	db, err := OpenNewestTypeDB(dir)
	if !errors.Is(err, ErrNoTypeDB) {
		t.Fatalf("OpenNewestTypeDB = %v, %v; want ErrNoTypeDB", db, err)
	}
	assertExists(t, partial)
}

// makeUnopenableTypeDB creates a type DB that looks ready (bolt file and
// index directory exist) but whose bolt file is garbage, as on a volume left
// by a crashed or foreign container.
func makeUnopenableTypeDB(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	touchFile(t, path)
	touchDir(t, path+typeDBIndexSuffix)
	return path
}

// captureLog sends the standard logger to a buffer for the rest of the test.
// Read the buffer only after every goroutine that logs has stopped.
// Not safe with t.Parallel: the logger and the buffer are shared unsynchronised.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

// TestNewStaticFetcherBuildsSynchronouslyWhenOldDBUnopenable pins start() for
// an old ready-looking DB that fails to open: the build runs before start()
// returns, as on an empty db_path, and the broken DB is removed once the new
// one is delivered.
func TestNewStaticFetcherBuildsSynchronouslyWhenOldDBUnopenable(t *testing.T) {
	dir := t.TempDir()
	broken := makeUnopenableTypeDB(t, dir, "types-100")
	srv, archiveRequests := sdeServer(t, http.StatusOK, serveFile(t, sampleZip(t)))
	rec := newDBRecorder(t)
	logs := captureLog(t)

	f := newTestFetcher(srv, dir, rec).start()
	assertCalls(t, rec, sdeLabel)
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := archiveRequests.Load(); got != 1 {
		t.Errorf("archive requests = %d, want 1", got)
	}
	assertMissing(t, broken, broken+typeDBIndexSuffix)
	if !strings.Contains(logs.String(), "ERROR: opening type DB "+broken) {
		t.Errorf("log lacks the open error for %s:\n%s", broken, logs)
	}
}

// TestNewStaticFetcherStartsWithoutTypesWhenOldDBUnopenableAndCCPDown pins
// that start() returns with no type DB delivered when the old DB fails to open
// and the manifest is unreachable; the broken DB stays on disk.
func TestNewStaticFetcherStartsWithoutTypesWhenOldDBUnopenableAndCCPDown(t *testing.T) {
	dir := t.TempDir()
	broken := makeUnopenableTypeDB(t, dir, "types-100")
	srv, _ := sdeServer(t, http.StatusServiceUnavailable, unexpectedArchive(t))
	rec := newDBRecorder(t)
	logs := captureLog(t)

	f := newTestFetcher(srv, dir, rec).start()
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertCalls(t, rec)
	assertExists(t, broken, broken+typeDBIndexSuffix)
	if !strings.Contains(logs.String(), "ERROR: failed to fetch static data from CCP:") {
		t.Errorf("log lacks the fetch error:\n%s", logs)
	}
}

// TestRunOnceNeverRebuildsUnopenableCurrentBuild pins a known defect: a
// broken DB named after the latest build counts as ready, so every run tries
// to open it again and fails without downloading, and the service stays
// without types until the broken files are deleted by hand.
func TestRunOnceNeverRebuildsUnopenableCurrentBuild(t *testing.T) {
	dir := t.TempDir()
	broken := makeUnopenableTypeDB(t, dir, "types-"+testBuild)
	srv, archiveRequests := sdeServer(t, http.StatusOK, unexpectedArchive(t))
	rec := newDBRecorder(t)
	f := newTestFetcher(srv, dir, rec)

	for range 2 {
		if err := f.RunOnce(); err == nil {
			t.Fatal("RunOnce: want error for an unopenable current build")
		}
	}
	assertCalls(t, rec)
	if got := archiveRequests.Load(); got != 0 {
		t.Errorf("archive requests = %d, want 0", got)
	}
	assertExists(t, broken, broken+typeDBIndexSuffix)
}

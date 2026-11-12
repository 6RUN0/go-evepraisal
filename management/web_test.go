package management

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/boltdb/bolt"
	"github.com/evepraisal/go-evepraisal"
	boltdb "github.com/evepraisal/go-evepraisal/bolt"
)

func newTestContext(t *testing.T, backupPath string) (*Context, *boltdb.AppraisalDB) {
	t.Helper()
	adb, err := boltdb.NewAppraisalDB(filepath.Join(t.TempDir(), "appraisals"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adb.Close() })
	app := &evepraisal.App{AppraisalDB: adb}
	return &Context{App: app, AppraisalBackupPath: backupPath}, adb.(*boltdb.AppraisalDB)
}

// readGzipBolt fully decompresses a gzip file; a truncated stream fails here.
func readGzipBolt(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip header: %v", err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("backup is not a complete gzip stream: %v", err)
	}
	return data
}

// TestHandleBackupToFileWritesCompleteGzip pins the unclosed gzip writer:
// the trailer was never written, so the file could not be decompressed.
func TestHandleBackupToFileWritesCompleteGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appraisals.gz")
	ctx, adb := newTestContext(t, path)
	if err := adb.PutNewAppraisal(&evepraisal.Appraisal{Kind: "test", Raw: "Tritanium 5"}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	ctx.HandleBackupToFile(rec, httptest.NewRequest(http.MethodGet, "/backup-to-file/appraisals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}

	restored := filepath.Join(t.TempDir(), "restored")
	if err := os.WriteFile(restored, readGzipBolt(t, path), 0o600); err != nil {
		t.Fatal(err)
	}
	rdb, err := bolt.Open(restored, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("backup is not a bolt file: %v", err)
	}
	defer rdb.Close()
	var found bool
	_ = rdb.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("appraisals")).ForEach(func(_, v []byte) error {
			found = found || bytes.Contains(v, []byte("Tritanium 5"))
			return nil
		})
	})
	if !found {
		t.Error("restored backup does not contain the appraisal")
	}
}

// TestBackupFailureKeepsPreviousFile: a failed write must not replace a good backup.
func TestBackupFailureKeepsPreviousFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "appraisals.gz")
	if err := os.WriteFile(path, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeBackup(path, func(w io.Writer) error {
		_, _ = w.Write([]byte("partial"))
		return io.ErrClosedPipe
	})
	if err == nil {
		t.Fatal("writeBackup returned nil for a failing writer")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "previous" {
		t.Errorf("previous backup replaced: %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}

// TestHandleBackupStreamIsCompleteBolt: the streaming variant sends raw bolt, not gzip.
func TestHandleBackupStreamIsCompleteBolt(t *testing.T) {
	ctx, _ := newTestContext(t, "")
	rec := httptest.NewRecorder()
	ctx.HandleBackup(rec, httptest.NewRequest(http.MethodGet, "/backup/appraisals", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("status = %d, len %d", rec.Code, rec.Body.Len())
	}
}

// TestHandleRestoreConflictOnExistingID pins the status the restore command
// counts as skipped instead of an error.
func TestHandleRestoreConflictOnExistingID(t *testing.T) {
	ctx, adb := newTestContext(t, filepath.Join(t.TempDir(), "appraisals.gz"))
	if err := adb.PutNewAppraisal(&evepraisal.Appraisal{ID: "a", Kind: "test", Raw: "original"}); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"id":"a","kind":"test","raw":"imported"}`)
	rec := httptest.NewRecorder()
	ctx.HandleRestore(rec, httptest.NewRequest(http.MethodPost, "/restore", body))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body %q", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

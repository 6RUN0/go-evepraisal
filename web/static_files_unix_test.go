//go:build unix

package web

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestExtraStaticFilesSkipsFIFO pins that a FIFO in the tree is skipped:
// reading it on start blocks until a writer appears.
func TestExtraStaticFilesSkipsFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Skipf("Mkfifo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := newBrandingContext(t)
	ctx.ExtraStaticFilePath = dir
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx.HTTPHandler()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTPHandler blocked on a FIFO in extra-static-file-path")
	}
	files, _, err := extraStaticFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["/a.txt"] == "" {
		t.Errorf("files = %v, want only /a.txt", files)
	}
}

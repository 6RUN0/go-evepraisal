package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExtraStaticFilesMissingDirNamesConfigKey(t *testing.T) {
	_, err := extraStaticFiles(filepath.Join(t.TempDir(), "absent"))
	if err == nil || !strings.Contains(err.Error(), "extra-static-file-path") {
		t.Errorf("err = %v, want an error naming extra-static-file-path", err)
	}
}

func TestExtraStaticFilesPathIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extraStaticFiles(file); err == nil || !strings.Contains(err.Error(), "extra-static-file-path") {
		t.Errorf("err = %v, want an error naming extra-static-file-path", err)
	}
}

// TestExtraStaticFilesRoutesKeepLeadingSlash pins the trailing slash case:
// TrimPrefix with "dir/" produced routes without the leading "/".
func TestExtraStaticFilesRoutesKeepLeadingSlash(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "sub/b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{dir, dir + "/"} {
		files, err := extraStaticFiles(root)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"/a.txt":     filepath.Join(dir, "a.txt"),
			"/sub/b.txt": filepath.Join(dir, "sub", "b.txt"),
		}
		if len(files) != len(want) {
			t.Errorf("root %q: files = %v, want %v", root, files, want)
		}
		for route, path := range want {
			if files[route] != path {
				t.Errorf("root %q: files[%q] = %q, want %q", root, route, files[route], path)
			}
		}
	}
}

// TestExtraStaticFilesSymlinkedRoot pins a symlink as the configured path:
// WalkDir does not follow a symlinked root, so no file under it was served.
func TestExtraStaticFilesSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "static")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	files, err := extraStaticFiles(link)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["/a.txt"] == "" {
		t.Fatalf("files = %v, want only /a.txt", files)
	}
}

// TestExtraStaticFileServedWithContentType pins the response headers of an
// extra static file: the type set on the request left CSS to sniffing, and
// browsers refuse a stylesheet served as text/plain.
func TestExtraStaticFileServedWithContentType(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.ExtraStaticFilePath = t.TempDir()
	css := "body { color: red; }\n"
	if err := os.WriteFile(filepath.Join(ctx.ExtraStaticFilePath, "x.css"), []byte(css), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ctx.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x.css", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != css {
		t.Fatalf("got %d %q, want 200 %q", rec.Code, rec.Body.String(), css)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// TestRobotsContentType pins the robots.txt type on the response, not the request.
func TestRobotsContentType(t *testing.T) {
	ctx := newBrandingContext(t)
	rec := httptest.NewRecorder()
	ctx.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// TestExtraStaticSymlinkOutsideRootNotServed pins that a symlink inside the
// root is skipped: it would serve any file the process can read.
func TestExtraStaticSymlinkOutsideRootNotServed(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.ExtraStaticFilePath = t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(ctx.ExtraStaticFilePath, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	code, body := get(t, ctx, "/leak.txt")
	if code != http.StatusNotFound || strings.Contains(body, "secret") {
		t.Errorf("got %d %q, want 404 without the file", code, body)
	}
}

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
	files, err := extraStaticFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["/a.txt"] == "" {
		t.Errorf("files = %v, want only /a.txt", files)
	}
}

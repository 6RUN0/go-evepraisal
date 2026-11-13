package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtraStaticFilesMissingDirNamesConfigKey(t *testing.T) {
	_, _, err := extraStaticFiles(filepath.Join(t.TempDir(), "absent"))
	if err == nil || !strings.Contains(err.Error(), "extra-static-file-path") {
		t.Errorf("err = %v, want an error naming extra-static-file-path", err)
	}
}

func TestExtraStaticFilesPathIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := extraStaticFiles(file); err == nil || !strings.Contains(err.Error(), "extra-static-file-path") {
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
		files, _, err := extraStaticFiles(root)
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
	files, _, err := extraStaticFiles(link)
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

// TestExtraStaticSymlinkInsideRootServed pins a link to a regular file inside
// the root, as a tree of links into a release directory: it is served at the
// route of the link.
func TestExtraStaticSymlinkInsideRootServed(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.ExtraStaticFilePath = t.TempDir()
	release := filepath.Join(ctx.ExtraStaticFilePath, "releases", "v2")
	if err := os.MkdirAll(release, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "app.css"), []byte("css"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("releases", "v2", "app.css"), filepath.Join(ctx.ExtraStaticFilePath, "app.css")); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ctx.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "css" {
		t.Fatalf("got %d %q, want 200 css", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", got)
	}
}

// TestExtraStaticFilesConfigMapLayout pins the layout of a Kubernetes
// ConfigMap mount: each file is a link through the "..data" link into a
// "..<timestamp>" directory. Each file is served once, at its own name.
func TestExtraStaticFilesConfigMapLayout(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, "..2026_11_13_10_00_00.123")
	if err := os.Mkdir(stamp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stamp, "x.css"), []byte("css"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(stamp), filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "x.css"), filepath.Join(dir, "x.css")); err != nil {
		t.Fatal(err)
	}
	files, skipped, err := extraStaticFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["/x.css"] != filepath.Join(stamp, "x.css") {
		t.Errorf("files = %v, want only /x.css read from %s", files, stamp)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
}

// TestExtraStaticFilesListsSkippedLinks pins the links reported for the
// warning: one out of the root and one to a directory inside it.
func TestExtraStaticFilesListsSkippedLinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	leak := filepath.Join(dir, "leak.txt")
	subLink := filepath.Join(dir, "sublink")
	if err := os.Symlink(outside, leak); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", subLink); err != nil {
		t.Fatal(err)
	}
	files, skipped, err := extraStaticFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
	if len(skipped) != 2 || skipped[0] != leak || skipped[1] != subLink {
		t.Errorf("skipped = %v, want [%s %s]", skipped, leak, subLink)
	}
}

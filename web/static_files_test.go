package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

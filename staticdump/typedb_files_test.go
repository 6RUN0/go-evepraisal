package staticdump

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/evepraisal/go-evepraisal/bolt"
	"github.com/evepraisal/go-evepraisal/typedb"
)

// fixtureTypeID marks fixture databases; the sample SDE has no such type.
const fixtureTypeID = 1

// makeReadyTypeDB builds a real one-type bolt+bleve database named name in dir.
func makeReadyTypeDB(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	db, err := bolt.NewTypeDB(path, true)
	if err != nil {
		t.Fatalf("NewTypeDB(%s): %v", path, err)
	}
	if err := db.PutTypes([]typedb.EveType{{ID: fixtureTypeID, Name: name}}); err != nil {
		t.Fatalf("PutTypes: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

func touchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
}

func touchDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	touchFile(t, filepath.Join(path, "store"))
}

func assertExists(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func assertMissing(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err = %v", p, err)
		}
	}
}

func assertNoGlob(t *testing.T, pattern string) {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected no %s, found %v", pattern, matches)
	}
}

func assertNewest(t *testing.T, dir, want string) {
	t.Helper()
	got, ok := findNewestTypeDB(dir)
	if !ok {
		t.Fatalf("findNewestTypeDB: ok=false, want %s", want)
	}
	if got != filepath.Join(dir, want) {
		t.Errorf("findNewestTypeDB = %s, want %s", got, filepath.Join(dir, want))
	}
}

func TestFindNewestTypeDBNumericOrder(t *testing.T) {
	dir := t.TempDir()
	touchFile(t, filepath.Join(dir, "types-99"))
	touchDir(t, filepath.Join(dir, "types-99.index"))
	touchFile(t, filepath.Join(dir, "types-100"))
	touchDir(t, filepath.Join(dir, "types-100.index"))
	assertNewest(t, dir, "types-100")
}

func TestFindNewestTypeDBRequiresIndex(t *testing.T) {
	dir := t.TempDir()
	touchFile(t, filepath.Join(dir, "types-100"))
	touchDir(t, filepath.Join(dir, "types-100.index"))
	touchFile(t, filepath.Join(dir, "types-200"))
	assertNewest(t, dir, "types-100")
}

func TestFindNewestTypeDBIgnoresPartial(t *testing.T) {
	dir := t.TempDir()
	touchFile(t, filepath.Join(dir, "types-100"))
	touchDir(t, filepath.Join(dir, "types-100.index"))
	touchFile(t, filepath.Join(dir, "types-300.partial"))
	touchDir(t, filepath.Join(dir, "types-300.partial.index"))
	assertNewest(t, dir, "types-100")
}

func TestFindNewestTypeDBLegacy(t *testing.T) {
	dir := t.TempDir()
	touchFile(t, filepath.Join(dir, "types-deadbeef"))
	touchDir(t, filepath.Join(dir, "types-deadbeef.index"))
	touchFile(t, filepath.Join(dir, "types-cafe"))
	touchDir(t, filepath.Join(dir, "types-cafe.index"))
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "types-cafe"), old, old); err != nil {
		t.Fatal(err)
	}
	assertNewest(t, dir, "types-deadbeef")
}

func TestFindNewestTypeDBNone(t *testing.T) {
	dir := t.TempDir()
	touchFile(t, filepath.Join(dir, "prices"))
	if got, ok := findNewestTypeDB(dir); ok {
		t.Errorf("findNewestTypeDB = %s, want ok=false", got)
	}
}

func TestRemoveStaleTypeDBs(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "types-3687575")
	touchFile(t, keep)
	touchDir(t, keep+".index")
	touchFile(t, keep+".zip")
	touchFile(t, filepath.Join(dir, "types-100"))
	touchDir(t, filepath.Join(dir, "types-100.index"))
	touchFile(t, filepath.Join(dir, "types-deadbeef"))
	touchDir(t, filepath.Join(dir, "types-deadbeef.index"))
	touchFile(t, filepath.Join(dir, "types-deadbeef.zip"))
	touchFile(t, filepath.Join(dir, "types-5.partial"))

	if err := removeStaleTypeDBs(dir, keep); err != nil {
		t.Fatalf("removeStaleTypeDBs: %v", err)
	}
	assertExists(t, keep, keep+".index", keep+".zip")
	assertMissing(t,
		filepath.Join(dir, "types-100"), filepath.Join(dir, "types-100.index"),
		filepath.Join(dir, "types-deadbeef"), filepath.Join(dir, "types-deadbeef.index"),
		filepath.Join(dir, "types-deadbeef.zip"), filepath.Join(dir, "types-5.partial"))
}

func TestRemoveStaleTypeDBsKeepsUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "types-200")
	touchFile(t, keep)
	touchDir(t, keep+".index")
	unrelated := []string{"prices", "appraisals", "httpcache", "typesfoo"}
	for _, name := range unrelated {
		touchFile(t, filepath.Join(dir, name))
	}
	touchDir(t, filepath.Join(dir, "backups"))

	if err := removeStaleTypeDBs(dir, keep); err != nil {
		t.Fatalf("removeStaleTypeDBs: %v", err)
	}
	for _, name := range append(unrelated, "backups") {
		assertExists(t, filepath.Join(dir, name))
	}
	assertExists(t, keep, keep+".index")
}

func TestRemoveStaleTypeDBsContinuesOnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so no path can be made undeletable")
	}
	dir := t.TempDir()
	keep := filepath.Join(dir, "types-200")
	touchFile(t, keep)
	touchDir(t, keep+".index")
	locked := filepath.Join(dir, "types-100.index")
	touchDir(t, locked)
	if err := os.Chmod(locked, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0700) })
	touchFile(t, filepath.Join(dir, "types-100"))
	touchFile(t, filepath.Join(dir, "types-deadbeef"))

	if err := removeStaleTypeDBs(dir, keep); err == nil {
		t.Fatal("removeStaleTypeDBs: want error for the locked path")
	}
	assertMissing(t, filepath.Join(dir, "types-100"), filepath.Join(dir, "types-deadbeef"))
	assertExists(t, keep, keep+".index")
}

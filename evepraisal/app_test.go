package main

import (
	"os"
	"path/filepath"
	"testing"

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

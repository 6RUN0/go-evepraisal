package bolt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/boltdb/bolt"
)

// TestNewTypeDBClosesBoltOnIndexError pins the bolt file left open when the
// bleve index fails to open: every retry of the first type DB build kept a
// file descriptor, a mapping and the file lock.
func TestNewTypeDBClosesBoltOnIndexError(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "types")
	db, err := bolt.Open(filename, 0o600, &bolt.Options{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// A regular file where bleve expects the index directory.
	if err := os.WriteFile(filename+".index", []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewTypeDB(filename, true); err == nil {
		t.Fatal("NewTypeDB succeeded on a broken index")
	}

	db, err = bolt.Open(filename, 0o600, &bolt.Options{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("bolt file still locked after NewTypeDB failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

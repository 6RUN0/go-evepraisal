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
// file descriptor, a mapping and the file lock. Both modes are covered, so a
// close moved into one of them fails the other subtest.
func TestNewTypeDBClosesBoltOnIndexError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		writable bool
		// index is written in place of the index directory, nil leaves it absent.
		index []byte
	}{
		{name: "writable broken index", writable: true, index: []byte("broken")},
		{name: "read-only missing index", writable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "types")
			db, err := bolt.Open(filename, 0o600, &bolt.Options{Timeout: 100 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if tc.index != nil {
				if err := os.WriteFile(filename+".index", tc.index, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := NewTypeDB(filename, tc.writable); err == nil {
				t.Fatal("NewTypeDB succeeded without a usable index")
			}

			// A writable open takes the exclusive lock, so it also fails on
			// the shared lock a leaked read-only handle keeps.
			db, err = bolt.Open(filename, 0o600, &bolt.Options{Timeout: 100 * time.Millisecond})
			if err != nil {
				t.Fatalf("bolt file still locked after NewTypeDB failed: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

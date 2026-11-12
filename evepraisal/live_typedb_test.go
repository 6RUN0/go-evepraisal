package main

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/evepraisal/go-evepraisal/typedb"
)

// closeCheckingTypeDB fails any lookup made after Close.
type closeCheckingTypeDB struct {
	typedb.TypeDB
	t      *testing.T
	id     int64
	closed atomic.Bool
}

func (db *closeCheckingTypeDB) GetTypeByID(int64) (typedb.EveType, bool) {
	if db.closed.Load() {
		db.t.Errorf("lookup on closed type DB %d", db.id)
	}
	return typedb.EveType{ID: db.id}, true
}

func (db *closeCheckingTypeDB) Close() error {
	db.closed.Store(true)
	return nil
}

// TestLiveTypeDBReplaceDuringLookups pins that the static fetcher callback can
// swap and close type DBs while handlers read them (run with -race).
func TestLiveTypeDBReplaceDuringLookups(t *testing.T) {
	live := &liveTypeDB{db: &closeCheckingTypeDB{t: t, id: 0}}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					live.GetTypeByID(34)
				}
			}
		}()
	}
	for id := int64(1); id <= 100; id++ {
		old := live.replace(&closeCheckingTypeDB{t: t, id: id})
		if err := old.Close(); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()

	if got, _ := live.GetTypeByID(34); got.ID != 100 {
		t.Errorf("current DB = %d, want 100", got.ID)
	}
}

// TestLiveTypeDBWithoutDB pins that requests made before the first type DB is
// delivered get "not found" or an error instead of a nil-interface panic.
func TestLiveTypeDBWithoutDB(t *testing.T) {
	live := &liveTypeDB{}

	t.Run("GetType", func(t *testing.T) {
		if ty, ok := live.GetType("Tritanium"); ok || ty.ID != 0 {
			t.Errorf("GetType = %v, %v; want not found", ty, ok)
		}
	})
	t.Run("HasType", func(t *testing.T) {
		if live.HasType("Tritanium") {
			t.Error("HasType = true, want false")
		}
	})
	t.Run("GetTypeByID", func(t *testing.T) {
		if ty, ok := live.GetTypeByID(34); ok || ty.ID != 0 {
			t.Errorf("GetTypeByID = %v, %v; want not found", ty, ok)
		}
	})
	t.Run("ListTypes", func(t *testing.T) {
		if types, err := live.ListTypes(0, 10); !errors.Is(err, errNoTypeDB) || len(types) != 0 {
			t.Errorf("ListTypes = %v, %v; want empty, errNoTypeDB", types, err)
		}
	})
	t.Run("PutTypes", func(t *testing.T) {
		if err := live.PutTypes([]typedb.EveType{{ID: 34}}); !errors.Is(err, errNoTypeDB) {
			t.Errorf("PutTypes = %v, want errNoTypeDB", err)
		}
	})
	t.Run("Search", func(t *testing.T) {
		if found := live.Search("Trit"); len(found) != 0 {
			t.Errorf("Search = %v, want empty", found)
		}
	})
	t.Run("Delete", func(t *testing.T) {
		if err := live.Delete(); !errors.Is(err, errNoTypeDB) {
			t.Errorf("Delete = %v, want errNoTypeDB", err)
		}
	})
	t.Run("Close", func(t *testing.T) {
		if err := live.Close(); err != nil {
			t.Errorf("Close = %v, want nil", err)
		}
	})
}

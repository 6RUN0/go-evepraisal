package main

import (
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

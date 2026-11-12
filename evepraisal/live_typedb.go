package main

import (
	"sync"

	"github.com/evepraisal/go-evepraisal/typedb"
)

// liveTypeDB is the TypeDB the web handlers and parsers hold for the whole
// process; replace swaps the backing DB while requests are in flight.
type liveTypeDB struct {
	mu sync.RWMutex
	db typedb.TypeDB
}

// replace installs db and returns the previous DB. No call uses the previous
// DB after replace returns, so the caller may close it.
func (l *liveTypeDB) replace(db typedb.TypeDB) typedb.TypeDB {
	l.mu.Lock()
	defer l.mu.Unlock()
	old := l.db
	l.db = db
	return old
}

// GetType delegates to the current DB.
func (l *liveTypeDB) GetType(typeName string) (typedb.EveType, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.db.GetType(typeName)
}

// HasType delegates to the current DB.
func (l *liveTypeDB) HasType(typeName string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.db.HasType(typeName)
}

// GetTypeByID delegates to the current DB.
func (l *liveTypeDB) GetTypeByID(typeID int64) (typedb.EveType, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.db.GetTypeByID(typeID)
}

// ListTypes delegates to the current DB.
func (l *liveTypeDB) ListTypes(startingTypeID int64, limit int64) ([]typedb.EveType, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.db.ListTypes(startingTypeID, limit)
}

// PutTypes delegates to the current DB.
func (l *liveTypeDB) PutTypes(types []typedb.EveType) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.db.PutTypes(types)
}

// Search delegates to the current DB.
func (l *liveTypeDB) Search(s string) []typedb.EveType {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.db.Search(s)
}

// Delete delegates to the current DB.
func (l *liveTypeDB) Delete() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.db.Delete()
}

// Close closes the current DB, if any.
func (l *liveTypeDB) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.db == nil {
		return nil
	}
	return l.db.Close()
}

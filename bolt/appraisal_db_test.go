package bolt

import (
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/boltdb/bolt"
	"github.com/evepraisal/go-evepraisal"
)

func newTestAppraisalDB(t *testing.T) evepraisal.AppraisalDB {
	t.Helper()
	db, err := NewAppraisalDB(filepath.Join(t.TempDir(), "appraisals"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func importAppraisal(t *testing.T, db evepraisal.AppraisalDB, id uint64, raw string) error {
	t.Helper()
	return db.PutNewAppraisal(&evepraisal.Appraisal{ID: evepraisal.Uint64ToAppraisalID(id), Kind: "test", Raw: raw})
}

// TestPutNewAppraisalAdvancesSequencePastImportedID pins the ID counter left
// behind an import: a later new appraisal took an imported ID and replaced it.
func TestPutNewAppraisalAdvancesSequencePastImportedID(t *testing.T) {
	db := newTestAppraisalDB(t)
	if err := importAppraisal(t, db, 10, "imported 10"); err != nil {
		t.Fatal(err)
	}
	// A lower imported ID must not move the counter back.
	if err := importAppraisal(t, db, 7, "imported 7"); err != nil {
		t.Fatal(err)
	}

	fresh := &evepraisal.Appraisal{Kind: "test", Raw: "new"}
	if err := db.PutNewAppraisal(fresh); err != nil {
		t.Fatal(err)
	}
	if got := evepraisal.AppraisalIDToUint64(fresh.ID); got != 11 {
		t.Fatalf("new appraisal ID = %q (%d), want 11", fresh.ID, got)
	}
	got, err := db.GetAppraisal(evepraisal.Uint64ToAppraisalID(10), false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Raw != "imported 10" {
		t.Fatalf("imported appraisal Raw = %q, want %q", got.Raw, "imported 10")
	}
}

// TestPutNewAppraisalRejectsExistingID pins the silent overwrite of a stored
// appraisal by an imported one with the same ID.
func TestPutNewAppraisalRejectsExistingID(t *testing.T) {
	db := newTestAppraisalDB(t)
	stored := &evepraisal.Appraisal{Kind: "test", Raw: "original"}
	if err := db.PutNewAppraisal(stored); err != nil {
		t.Fatal(err)
	}

	err := db.PutNewAppraisal(&evepraisal.Appraisal{ID: stored.ID, Kind: "test", Raw: "imported"})
	if !errors.Is(err, evepraisal.ErrAppraisalExists) {
		t.Fatalf("err = %v, want ErrAppraisalExists", err)
	}
	got, err := db.GetAppraisal(stored.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Raw != "original" {
		t.Fatalf("stored appraisal Raw = %q, want %q", got.Raw, "original")
	}
}

func setAppraisalSequence(t *testing.T, db evepraisal.AppraisalDB, seq uint64) {
	t.Helper()
	err := db.(*AppraisalDB).DB.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("appraisals")).SetSequence(seq)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func requireRaw(t *testing.T, db evepraisal.AppraisalDB, id, want string) {
	t.Helper()
	got, err := db.GetAppraisal(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Raw != want {
		t.Fatalf("appraisal %q Raw = %q, want %q", id, got.Raw, want)
	}
}

// TestNewAppraisalDBRaisesSequenceToStoredKeys pins volumes restored without
// the import counter bump: the next new appraisal reused a stored ID.
func TestNewAppraisalDBRaisesSequenceToStoredKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appraisals")
	raw, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = raw.Update(func(tx *bolt.Tx) error {
		b, cerr := tx.CreateBucket([]byte("appraisals"))
		if cerr != nil {
			return cerr
		}
		return b.Put(EncodeDBIDFromUint64(100), []byte("stored"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := NewAppraisalDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fresh := &evepraisal.Appraisal{Kind: "test", Raw: "new"}
	if err := db.PutNewAppraisal(fresh); err != nil {
		t.Fatal(err)
	}
	if got := evepraisal.AppraisalIDToUint64(fresh.ID); got != 101 {
		t.Fatalf("new appraisal ID = %q (%d), want 101", fresh.ID, got)
	}
}

// TestPutNewAppraisalRefusesTakenSequenceID pins the silent overwrite of a
// stored appraisal when the counter lags behind the stored keys.
func TestPutNewAppraisalRefusesTakenSequenceID(t *testing.T) {
	db := newTestAppraisalDB(t)
	if err := importAppraisal(t, db, 5, "imported 5"); err != nil {
		t.Fatal(err)
	}
	setAppraisalSequence(t, db, 4)

	err := db.PutNewAppraisal(&evepraisal.Appraisal{Kind: "test", Raw: "new"})
	if !errors.Is(err, errAppraisalSequenceBehind) {
		t.Fatalf("err = %v, want errAppraisalSequenceBehind", err)
	}
	requireRaw(t, db, evepraisal.Uint64ToAppraisalID(5), "imported 5")
}

// TestPutNewAppraisalRejectsImportNearMaxUint64 pins the counter wrap: an
// imported ID near MaxUint64 raised the sequence, and NextSequence went to 0.
func TestPutNewAppraisalRejectsImportNearMaxUint64(t *testing.T) {
	db := newTestAppraisalDB(t)
	stored := &evepraisal.Appraisal{Kind: "test", Raw: "original"}
	if err := db.PutNewAppraisal(stored); err != nil {
		t.Fatal(err)
	}

	err := importAppraisal(t, db, math.MaxUint64-1, "imported")
	if !errors.Is(err, errImportedIDOutOfRange) {
		t.Fatalf("err = %v, want errImportedIDOutOfRange", err)
	}
	fresh := &evepraisal.Appraisal{Kind: "test", Raw: "new"}
	if err := db.PutNewAppraisal(fresh); err != nil {
		t.Fatal(err)
	}
	if got := evepraisal.AppraisalIDToUint64(fresh.ID); got != 2 {
		t.Fatalf("new appraisal ID = %q (%d), want 2", fresh.ID, got)
	}
	requireRaw(t, db, stored.ID, "original")
}

// TestPutNewAppraisalImportCeilingBoundary pins the ceiling itself: an
// off-by-one there either refuses valid IDs or lets the counter near the wrap.
func TestPutNewAppraisalImportCeilingBoundary(t *testing.T) {
	db := newTestAppraisalDB(t)
	if err := importAppraisal(t, db, maxImportedAppraisalID, "at ceiling"); err != nil {
		t.Fatalf("import at ceiling: %v", err)
	}
	err := importAppraisal(t, db, maxImportedAppraisalID+1, "past ceiling")
	if !errors.Is(err, errImportedIDOutOfRange) {
		t.Fatalf("err = %v, want errImportedIDOutOfRange", err)
	}
	requireRaw(t, db, evepraisal.Uint64ToAppraisalID(maxImportedAppraisalID), "at ceiling")
}

// TestPutNewAppraisalRefusesSequenceWrap pins NextSequence wrapping from
// MaxUint64 to 0, which handed out the oldest IDs again.
func TestPutNewAppraisalRefusesSequenceWrap(t *testing.T) {
	db := newTestAppraisalDB(t)
	setAppraisalSequence(t, db, math.MaxUint64)

	err := db.PutNewAppraisal(&evepraisal.Appraisal{Kind: "test", Raw: "new"})
	if !errors.Is(err, errAppraisalSequenceOverflow) {
		t.Fatalf("err = %v, want errAppraisalSequenceOverflow", err)
	}
}

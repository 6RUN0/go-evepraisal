package bolt

import (
	"errors"
	"path/filepath"
	"testing"

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

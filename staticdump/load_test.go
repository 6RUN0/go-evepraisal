package staticdump

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/evepraisal/go-evepraisal/bolt"
	"github.com/evepraisal/go-evepraisal/typedb"
)

// realSDEControlTypeID is a published type with a marketGroupID that is absent
// from the 2025-07-07 YAML SDE (fsd/types.yaml); it proves the JSONL archive
// is newer than the data the old loader read.
const realSDEControlTypeID = 98524

func mustReadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return b
}

// writeSDEZip builds an archive with the given files and returns its path.
func writeSDEZip(t *testing.T, files map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sde.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func sampleZip(t *testing.T) string {
	return writeSDEZip(t, map[string][]byte{
		"types.jsonl":      mustReadTestdata(t, "types-sample.jsonl"),
		"blueprints.jsonl": mustReadTestdata(t, "blueprints-sample.jsonl"),
	})
}

func typesByID(types []typedb.EveType) map[int64]typedb.EveType {
	m := make(map[int64]typedb.EveType, len(types))
	for _, ty := range types {
		m[ty.ID] = ty
	}
	return m
}

func TestLoadTypesFromRealSampleLines(t *testing.T) {
	types, err := loadtypes(sampleZip(t))
	if err != nil {
		t.Fatal(err)
	}
	drake, ok := typesByID(types)[24698]
	if !ok {
		t.Fatal("Drake not loaded")
	}
	if drake.Name != "Drake" || drake.Volume != 252000 || drake.PackagedVolume != 15000 ||
		drake.GroupID != 419 || drake.MarketGroupID != 471 || drake.BasePrice != 40400000 {
		t.Errorf("unexpected Drake: %+v", drake)
	}
	noPackaged := typesByID(types)[98270]
	if noPackaged.PackagedVolume != 0 {
		t.Errorf("type without packagedVolume: got %v, want 0", noPackaged.PackagedVolume)
	}
	if _, ok := typesByID(types)[0]; !ok {
		t.Error("unpublished named type must still be loaded")
	}
}

func TestLoadTypesSkipsUnnamed(t *testing.T) {
	types := `{"_key": 1, "name": {"en": ""}}
{"_key": 2}
{"_key": 3, "name": {"en": "  Foo "}}
`
	path := writeSDEZip(t, map[string][]byte{"types.jsonl": []byte(types), "blueprints.jsonl": nil})
	got, err := loadtypes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 3 || got[0].Name != "Foo" {
		t.Errorf("got %+v, want only type 3 named Foo", got)
	}
}

func TestLoadTypesBlueprintComponents(t *testing.T) {
	// 11134 is the product of blueprint 11135 in the real sample.
	path := writeSDEZip(t, map[string][]byte{
		"types.jsonl":      []byte(`{"_key": 11134, "name": {"en": "Kestrel"}}` + "\n"),
		"blueprints.jsonl": mustReadTestdata(t, "blueprints-sample.jsonl"),
	})
	got, err := loadtypes(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []typedb.Component{{Quantity: 2778, TypeID: 34}}
	if len(got) != 1 || !reflect.DeepEqual(got[0].Components, want) {
		t.Errorf("Components: got %+v, want %+v", got, want)
	}
}

func TestLoadTypesBlueprintWithoutManufacturing(t *testing.T) {
	path := writeSDEZip(t, map[string][]byte{
		"types.jsonl":      []byte(`{"_key": 935, "name": {"en": "Foo"}}` + "\n"),
		"blueprints.jsonl": mustReadTestdata(t, "blueprints-sample.jsonl"),
	})
	got, err := loadtypes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Components != nil {
		t.Errorf("got %+v, want nil Components", got)
	}
}

func TestLoadTypesFirstBlueprintWins(t *testing.T) {
	bps := `{"_key": 1, "blueprintTypeID": 1, "activities": {"manufacturing": {"materials": [{"quantity": 5, "typeID": 34}], "products": [{"quantity": 1, "typeID": 100}]}}}
{"_key": 2, "blueprintTypeID": 2, "activities": {"manufacturing": {"materials": [{"quantity": 9, "typeID": 35}], "products": [{"quantity": 1, "typeID": 100}]}}}
`
	path := writeSDEZip(t, map[string][]byte{
		"types.jsonl":      []byte(`{"_key": 100, "name": {"en": "Foo"}}` + "\n"),
		"blueprints.jsonl": []byte(bps),
	})
	want := []typedb.Component{{Quantity: 5, TypeID: 34}}
	for i := 0; i < 20; i++ {
		got, err := loadtypes(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0].Components, want) {
			t.Fatalf("run %d: got %+v, want %+v", i, got, want)
		}
	}
}

func TestLoadTypesMissingFile(t *testing.T) {
	path := writeSDEZip(t, map[string][]byte{"types.jsonl": []byte(`{"_key": 1, "name": {"en": "A"}}` + "\n")})
	_, err := loadtypes(path)
	if err == nil || !strings.Contains(err.Error(), "blueprints.jsonl") {
		t.Errorf("got %v, want error naming blueprints.jsonl", err)
	}
}

func TestLoadTypesBadLine(t *testing.T) {
	path := writeSDEZip(t, map[string][]byte{
		"types.jsonl":      []byte(`{"_key": 1, "name": {"en": "A"}}` + "\n\n{\n"),
		"blueprints.jsonl": nil,
	})
	_, err := loadtypes(path)
	if err == nil || !strings.Contains(err.Error(), "types.jsonl") || !strings.Contains(err.Error(), "line 3") {
		t.Errorf("got %v, want error with types.jsonl and line 3", err)
	}
}

func openZip(t *testing.T, path string) *zip.ReadCloser {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestForEachJSONLineAcceptsLongLine(t *testing.T) {
	long := strings.Repeat("a", 200*1024)
	r := openZip(t, writeSDEZip(t, map[string][]byte{"x.jsonl": []byte("{}\n" + long + "\n")}))
	var lens []int
	err := forEachJSONLine(r, "x.jsonl", func(_ int, line []byte) error {
		lens = append(lens, len(line))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lens, []int{2, len(long)}) {
		t.Errorf("line lengths: got %v", lens)
	}
}

func TestForEachJSONLineRejectsOverlongLine(t *testing.T) {
	long := strings.Repeat("a", 2*1024*1024)
	r := openZip(t, writeSDEZip(t, map[string][]byte{"x.jsonl": []byte("{}\n" + long + "\n")}))
	err := forEachJSONLine(r, "x.jsonl", func(int, []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("got %v, want error with line 2", err)
	}
}

func TestLoadTypesRoundTripThroughBolt(t *testing.T) {
	types, err := loadtypes(sampleZip(t))
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "types-1")
	db, err := bolt.NewTypeDB(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutTypes(types); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ro, err := bolt.NewTypeDB(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	drake, ok := ro.GetTypeByID(24698)
	if !ok || drake.PackagedVolume != 15000 {
		t.Errorf("Drake after round trip: ok=%v %+v", ok, drake)
	}
}

// TestLoadTypesRealSDE runs against a real archive; opt-in via EVEPRAISAL_SDE_ZIP.
func TestLoadTypesRealSDE(t *testing.T) {
	path := os.Getenv("EVEPRAISAL_SDE_ZIP")
	if path == "" {
		t.Skip("EVEPRAISAL_SDE_ZIP not set")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("EVEPRAISAL_SDE_ZIP=%s: %v", path, err)
	}
	types, err := loadtypes(path)
	if err != nil {
		t.Fatal(err)
	}
	byID := typesByID(types)
	withVolume := 0
	for _, ty := range types {
		if ty.Volume > 0 {
			withVolume++
		}
	}
	t.Logf("types=%d withVolume=%d", len(types), withVolume)
	if len(types) < 50000 {
		t.Errorf("types: got %d, want >= 50000", len(types))
	}
	if byID[24698].PackagedVolume != 15000 {
		t.Errorf("Drake PackagedVolume: got %v", byID[24698].PackagedVolume)
	}
	if withVolume*100 <= len(types)*85 {
		t.Errorf("types with Volume > 0: %d of %d, want > 85 percent (88.4 measured on build 3687575)", withVolume, len(types))
	}
	control, ok := byID[realSDEControlTypeID]
	if !ok || control.MarketGroupID == 0 {
		t.Errorf("control type %d missing or without market group: %+v", realSDEControlTypeID, control)
	}
}

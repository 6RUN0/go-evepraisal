package staticdump

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A type database n lives on disk as three names: the bolt file types-<n>,
// the bleve index types-<n>.index and the archive cache types-<n>.zip.
// It is built under types-<n>.partial and renamed once complete.
const (
	typeDBPrefix       = "types-"
	typeDBIndexSuffix  = ".index"
	typeDBCacheSuffix  = ".zip"
	typeDBPartial      = ".partial"
	typeDBDownloadPart = ".download"
)

// isReadyTypeDB reports whether both the bolt file and its index exist.
func isReadyTypeDB(path string) bool {
	db, err := os.Stat(path)
	if err != nil || db.IsDir() {
		return false
	}
	_, err = os.Stat(path + typeDBIndexSuffix)
	return err == nil
}

// findNewestTypeDB returns the newest ready type DB in dbPath: the highest
// numeric build first, else the most recently modified legacy types-<md5>.
func findNewestTypeDB(dbPath string) (path string, ok bool) {
	entries, err := os.ReadDir(dbPath)
	if err != nil {
		return "", false
	}

	var (
		bestBuild   int64 = -1
		bestNumeric string
		bestLegacy  string
		legacyMTime time.Time
	)
	for _, entry := range entries {
		name := entry.Name()
		suffix, found := strings.CutPrefix(name, typeDBPrefix)
		if !found || suffix == "" || strings.Contains(suffix, ".") {
			continue
		}
		candidate := filepath.Join(dbPath, name)
		if !isReadyTypeDB(candidate) {
			continue
		}
		if build, err := strconv.ParseInt(suffix, 10, 64); err == nil {
			if build > bestBuild {
				bestBuild = build
				bestNumeric = candidate
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if bestLegacy == "" || info.ModTime().After(legacyMTime) {
			bestLegacy = candidate
			legacyMTime = info.ModTime()
		}
	}

	if bestNumeric != "" {
		return bestNumeric, true
	}
	if bestLegacy != "" {
		return bestLegacy, true
	}
	return "", false
}

// removeStaleTypeDBs removes every types-* entry except the three names of
// keepPath. It keeps going after a failure and returns all failures joined.
func removeStaleTypeDBs(dbPath, keepPath string) error {
	keepName := filepath.Base(keepPath)
	keep := map[string]bool{
		keepName:                     true,
		keepName + typeDBIndexSuffix: true,
		keepName + typeDBCacheSuffix: true,
	}
	return removeTypeDBEntries(dbPath, func(name string) bool { return !keep[name] })
}

// removePartialTypeDBs removes builds left unfinished by a killed process.
func removePartialTypeDBs(dbPath string) error {
	return removeTypeDBEntries(dbPath, func(name string) bool {
		return strings.Contains(name, typeDBPartial)
	})
}

func removeTypeDBEntries(dbPath string, shouldRemove func(name string) bool) error {
	entries, err := os.ReadDir(dbPath)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, typeDBPrefix) || !shouldRemove(name) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dbPath, name)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

package staticdump

import (
	"archive/zip"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/evepraisal/go-evepraisal/bolt"
	"github.com/evepraisal/go-evepraisal/typedb"
	"github.com/sethgrid/pester"
)

// errStopped aborts a type DB build when the fetcher is closed.
var errStopped = errors.New("static fetcher stopped")

// StaticFetcher continually fetches a new static dump and updates the type database with the current types
type StaticFetcher struct {
	dbPath   string
	callback func(typeDB typedb.TypeDB)
	client   *pester.Client

	manifestURL string
	// cleanup removes older type DBs after a new one is delivered; restore runs without it.
	cleanup bool

	// runMu serialises RunOnce and guards currentPath, the DB last handed to callback.
	runMu       sync.Mutex
	currentPath string

	stop chan bool
	wg   *sync.WaitGroup
}

// NewStaticFetcher delivers a type DB to callback and keeps it current. With a
// ready DB on disk it is delivered at once and the newest build is fetched in
// the background; on an empty db_path the first build is fetched synchronously.
// cleanup=false keeps older type DBs on disk.
func NewStaticFetcher(client *pester.Client, dbPath string, cleanup bool, callback func(typeDB typedb.TypeDB)) (*StaticFetcher, error) {
	return newStaticFetcher(client, dbPath, DefaultManifestURL, cleanup, callback).start(), nil
}

func newStaticFetcher(client *pester.Client, dbPath, manifestURL string, cleanup bool, callback func(typeDB typedb.TypeDB)) *StaticFetcher {
	return &StaticFetcher{
		dbPath:   dbPath,
		callback: callback,
		client:   client,

		manifestURL: manifestURL,
		cleanup:     cleanup,

		stop: make(chan bool),
		wg:   &sync.WaitGroup{},
	}
}

func (f *StaticFetcher) start() *StaticFetcher {
	if err := removePartialTypeDBs(f.dbPath); err != nil {
		log.Printf("ERROR: removing unfinished type DBs: %s", err)
	}

	servedFromDisk := false
	if path, ok := findNewestTypeDB(f.dbPath); ok {
		if err := f.deliver(path); err != nil {
			log.Printf("ERROR: opening type DB %s: %s", path, err)
		} else {
			servedFromDisk = true
			log.Printf("Serving type DB %s while checking for a newer build", path)
		}
	}
	if !servedFromDisk {
		if err := f.RunOnce(); err != nil {
			log.Printf("ERROR: failed to fetch static data from CCP: %s", err)
		}
	}

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		if servedFromDisk {
			if err := f.RunOnce(); err != nil {
				log.Printf("ERROR: failed to fetch static data from CCP, keeping the current type DB: %s", err)
			}
		}
		for {
			select {
			case <-time.After(6 * time.Hour):
			case <-f.stop:
				return
			}

			if err := f.RunOnce(); err != nil {
				log.Printf("WARNING: Fetcher failed to run, keeping the current type DB: %s", err)
			}
		}
	}()

	return f
}

// RunOnce delivers the latest SDE build to the callback unless it is already
// current. On any failure the current DB stays in place and the broken
// archive cache and partial build are removed, so the next run starts clean.
func (f *StaticFetcher) RunOnce() error {
	f.runMu.Lock()
	defer f.runMu.Unlock()

	dump, err := FindLatestStaticDump(f.client, f.manifestURL)
	if err != nil {
		return fmt.Errorf("finding latest static dump: %w", err)
	}
	log.Println("Latest Static Dump", dump.URL, dump.BuildNumber)

	typedbPath := filepath.Join(f.dbPath, typeDBPrefix+strconv.FormatInt(dump.BuildNumber, 10))
	if typedbPath == f.currentPath {
		log.Println("Type DB is up to date", typedbPath)
		return nil
	}

	if !isReadyTypeDB(typedbPath) {
		if err := f.buildTypeDB(typedbPath, dump.URL); err != nil {
			return fmt.Errorf("loading types: %w", err)
		}
		log.Println("Done loading types", typedbPath)
	}

	if err := f.deliver(typedbPath); err != nil {
		return err
	}

	if f.cleanup {
		if err := removeStaleTypeDBs(f.dbPath, typedbPath); err != nil {
			log.Printf("ERROR: removing old type DBs: %s", err)
		}
	}
	return nil
}

// deliver opens a ready DB read-only and hands it to the callback.
func (f *StaticFetcher) deliver(typedbPath string) error {
	typeDB, err := bolt.NewTypeDB(typedbPath, false)
	if err != nil {
		return fmt.Errorf("NewTypeDB: %w", err)
	}
	f.callback(typeDB)
	f.currentPath = typedbPath
	return nil
}

// buildTypeDB downloads (or reuses) the archive and builds typedbPath via a
// .partial build. A failed build leaves neither the partial DB nor a bad cache.
func (f *StaticFetcher) buildTypeDB(typedbPath, staticDumpURL string) error {
	cachePath := typedbPath + typeDBCacheSuffix
	partialPath := typedbPath + typeDBPartial
	err := f.loadTypes(partialPath, cachePath, staticDumpURL)
	if err == nil {
		// Leftovers of a non-ready DB would make the renames below fail.
		err = errors.Join(os.RemoveAll(typedbPath), os.RemoveAll(typedbPath+typeDBIndexSuffix))
	}
	if err == nil {
		err = os.Rename(partialPath+typeDBIndexSuffix, typedbPath+typeDBIndexSuffix)
	}
	if err == nil {
		err = os.Rename(partialPath, typedbPath)
	}
	if err != nil {
		if rmErr := errors.Join(os.RemoveAll(partialPath), os.RemoveAll(partialPath+typeDBIndexSuffix)); rmErr != nil {
			log.Printf("ERROR: removing partial type DB %s: %s", partialPath, rmErr)
		}
		return err
	}
	return nil
}

// Close cleans up the worker
func (f *StaticFetcher) Close() error {
	close(f.stop)
	f.wg.Wait()
	return nil
}

func (f *StaticFetcher) loadTypes(typedbPath, staticCacheFile string, staticDumpURL string) error {
	// Reuse the cached archive only if its zip directory is readable.
	if r, err := zip.OpenReader(staticCacheFile); err == nil {
		_ = r.Close()
	} else {
		if rmErr := os.Remove(staticCacheFile); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("removing unreadable archive cache: %w", rmErr)
		}
		log.Printf("Downloading static dump to %s", staticCacheFile)
		if err := downloadTypes(f.client, staticDumpURL, staticCacheFile); err != nil {
			return err
		}
	}

	types, err := loadtypes(staticCacheFile)
	if err != nil {
		if rmErr := os.Remove(staticCacheFile); rmErr != nil {
			log.Printf("ERROR: removing unusable archive cache %s: %s", staticCacheFile, rmErr)
		}
		return err
	}

	typeDB, err := bolt.NewTypeDB(typedbPath, true)
	if err != nil {
		return fmt.Errorf("NewTypeDB: %w", err)
	}

	chunkSize := 1000
	for i := 0; i < len(types); i += chunkSize {
		end := min(i+chunkSize, len(types))

		select {
		case <-f.stop:
			err = errStopped
		default:
			log.Printf("Indexed %d/%d types", i, len(types))
			chunk := make([]typedb.EveType, end-i)
			copy(chunk, types[i:end])
			err = typeDB.PutTypes(chunk)
		}
		if err != nil {
			_ = typeDB.Close()
			return err
		}
	}

	if err := typeDB.Close(); err != nil {
		return fmt.Errorf("closing typeDB: %w", err)
	}
	log.Println("Finished typedb fetch")
	return nil
}

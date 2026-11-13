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

	boltdb "github.com/boltdb/bolt"
	"github.com/evepraisal/go-evepraisal/bolt"
	"github.com/evepraisal/go-evepraisal/typedb"
	"github.com/sethgrid/pester"
)

// errStopped aborts a type DB build when the fetcher is closed.
var errStopped = errors.New("static fetcher stopped")

// ErrNoTypeDB means db_path holds no ready type DB.
var ErrNoTypeDB = errors.New("no ready type DB")

const (
	// refreshInterval is the pause between checks for a newer SDE build.
	refreshInterval = 6 * time.Hour
	// firstRetryInterval is the first pause after a failed run while no type DB
	// is served; it doubles on every failure up to refreshInterval.
	firstRetryInterval = 5 * time.Minute
)

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

	// after is time.After; tests replace it to observe pauses without waiting.
	after func(time.Duration) <-chan time.Time

	stop chan bool
	wg   *sync.WaitGroup
}

// OpenNewestTypeDB opens the newest ready type DB in dbPath read-only without
// building or removing anything; it returns ErrNoTypeDB if there is none.
func OpenNewestTypeDB(dbPath string) (typedb.TypeDB, error) {
	path, ok := findNewestTypeDB(dbPath)
	if !ok {
		return nil, fmt.Errorf("%w in %s", ErrNoTypeDB, dbPath)
	}
	typeDB, err := bolt.NewTypeDB(path, false)
	if err != nil {
		return nil, fmt.Errorf("opening type DB %s: %w", path, err)
	}
	return typeDB, nil
}

// NewStaticFetcher delivers a type DB to callback and keeps it current. With a
// ready DB on disk that opens (the newest such) it is delivered at once and the
// newest build is fetched in the background; with none, the first build is
// fetched synchronously.
// Until a DB is delivered, runs repeat after firstRetryInterval, doubling up
// to refreshInterval. cleanup=false keeps older type DBs on disk.
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

		after: time.After,

		stop: make(chan bool),
		wg:   &sync.WaitGroup{},
	}
}

func (f *StaticFetcher) start() *StaticFetcher {
	if err := removePartialTypeDBs(f.dbPath); err != nil {
		log.Printf("ERROR: removing unfinished type DBs: %s", err)
	}

	// An older build that opens keeps the service serving while the newest
	// one, if broken, is rebuilt in the background by RunOnce.
	servedFromDisk := false
	for _, path := range findReadyTypeDBs(f.dbPath) {
		if err := f.deliver(path); err != nil {
			log.Printf("ERROR: opening type DB %s: %s", path, err)
			continue
		}
		servedFromDisk = true
		log.Printf("Serving type DB %s while checking for a newer build", path)
		break
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
		retryInterval := firstRetryInterval
		for {
			wait := refreshInterval
			if !f.hasDelivered() {
				wait = retryInterval
				retryInterval = min(2*retryInterval, refreshInterval)
			}
			select {
			case <-f.after(wait):
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
// current; a DB of that build that fails to open is rebuilt. On any failure the current DB stays in place and the broken
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

	built := false
	if !isReadyTypeDB(typedbPath) {
		if err := f.buildTypeDB(typedbPath, dump.URL); err != nil {
			return fmt.Errorf("loading types: %w", err)
		}
		built = true
		log.Println("Done loading types", typedbPath)
	}

	err = f.deliver(typedbPath)
	// A ready-looking DB that fails to open is rebuilt, else every run would
	// fail on it forever. ErrTimeout means another process holds it open for
	// writing: the files are left alone and the next run tries again. The
	// broken DB is replaced only after a successful build (see buildTypeDB).
	if err != nil && !built && !errors.Is(err, boltdb.ErrTimeout) {
		log.Printf("WARNING: rebuilding type DB that fails to open path=%s error=%q", typedbPath, err)
		if err := f.buildTypeDB(typedbPath, dump.URL); err != nil {
			return fmt.Errorf("rebuilding types: %w", err)
		}
		log.Println("Done loading types", typedbPath)
		err = f.deliver(typedbPath)
	}
	if err != nil {
		return err
	}

	if f.cleanup {
		if err := removeStaleTypeDBs(f.dbPath, typedbPath); err != nil {
			log.Printf("ERROR: removing old type DBs: %s", err)
		}
	}
	return nil
}

// hasDelivered reports whether a type DB has reached the callback.
func (f *StaticFetcher) hasDelivered() bool {
	f.runMu.Lock()
	defer f.runMu.Unlock()
	return f.currentPath != ""
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

package staticdump

import (
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

// StaticFetcher continually fetches a new static dump and updates the type database with the current types
type StaticFetcher struct {
	dbPath   string
	callback func(typeDB typedb.TypeDB)
	client   *pester.Client

	manifestURL string

	stop chan bool
	wg   *sync.WaitGroup
}

// NewStaticFetcher returns a new static data fetcher
func NewStaticFetcher(client *pester.Client, dbPath string, callback func(typeDB typedb.TypeDB)) (*StaticFetcher, error) {
	fetcher := &StaticFetcher{
		dbPath:   dbPath,
		callback: callback,
		client:   client,

		manifestURL: DefaultManifestURL,

		stop: make(chan bool),
		wg:   &sync.WaitGroup{},
	}

	err := fetcher.RunOnce()
	if err != nil {
		log.Printf("ERROR: failed to fetch static data from CCP: %s", err)
	}

	fetcher.wg.Add(1)
	go func() {
		defer fetcher.wg.Done()
		for {

			select {
			case <-time.After(6 * time.Hour):
			case <-fetcher.stop:
				return
			}

			err := fetcher.RunOnce()
			if err != nil {
				log.Printf("WARNING: Fetcher failed to run: %s", err)
			}

			log.Println("FETCHER RAN")
		}
	}()

	return fetcher, nil
}

// RunOnce will fetch, parse and call the callback with a fresh type database
func (f *StaticFetcher) RunOnce() error {
	dump, err := FindLatestStaticDump(f.client, f.manifestURL)
	if err != nil {
		// TODO: fallback to previously downloaded static data
		return fmt.Errorf("finding latest static dump: %w", err)
	}

	log.Println("Latest Static Dump", dump.URL, dump.BuildNumber)

	build := strconv.FormatInt(dump.BuildNumber, 10)
	staticDumpURL := dump.URL
	typedbPath := filepath.Join(f.dbPath, "types-"+build)
	typedbCachePath := filepath.Join(f.dbPath, "types-"+build+".zip")
	if _, err = os.Stat(typedbPath); os.IsNotExist(err) {
		err = f.loadTypes(typedbPath, typedbCachePath, staticDumpURL)
		if err != nil {
			return fmt.Errorf("loading types: %w", err)
		}
	} else if err != nil {
		return err
	}

	log.Println("Done loading types", typedbPath)

	typeDB, err := bolt.NewTypeDB(typedbPath, false)
	if err != nil {
		return fmt.Errorf("NewTypeDB: %w", err)
	}
	log.Println("done making new typedb")

	f.callback(typeDB)
	return nil
}

// Close cleans up the worker
func (f *StaticFetcher) Close() error {
	close(f.stop)
	f.wg.Wait()
	return nil
}

func (f *StaticFetcher) loadTypes(typedbPath, staticCacheFile string, staticDumpURL string) error {
	// avoid re-downloading the entire static dump if we already have it
	if _, err := os.Stat(staticCacheFile); os.IsNotExist(err) {
		log.Printf("Downloading static dump to %s", staticCacheFile)
		err = downloadTypes(f.client, staticDumpURL, staticCacheFile)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	types, err := loadtypes(staticCacheFile)
	if err != nil {
		return err
	}

	typeDB, err := bolt.NewTypeDB(typedbPath, true)
	if err != nil {
		return fmt.Errorf("NewTypeDB: %w", err)
	}
	finished := false
	defer func() {
		if finished {
			log.Println("Finished typedb fetch")
			err := typeDB.Close()
			if err != nil {
				log.Printf("ERROR: closing typeDB: %s", err)
			}
		} else {
			log.Println("Deleting new typedb because it was stopped before finishing")
			err = typeDB.Delete()
			if err != nil {
				log.Printf("Error deleting old typedb: %s", err)
			}
		}
	}()

	chunkSize := 1000
	for i := 0; i < len(types); i += chunkSize {
		end := i + chunkSize

		if end > len(types) {
			end = len(types)
		}

		if i%1000 == 0 {
			select {
			case <-f.stop:
				return nil
			default:
				log.Printf("Indexed %d/%d types", i, len(types))
			}
		}

		chunk := make([]typedb.EveType, len(types[i:end]))
		copy(chunk, types[i:end])

		err = typeDB.PutTypes(chunk)
		if err != nil {
			return err
		}
	}
	finished = true
	return nil
}

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/evepraisal/go-evepraisal"
	"github.com/evepraisal/go-evepraisal/legacy"
	"github.com/evepraisal/go-evepraisal/staticdump"
	"github.com/spf13/viper"
)

func restoreMain() {
	restoreCmd := flag.NewFlagSet("restore", flag.ExitOnError)
	filenamesStr := restoreCmd.String("files", "", "comma-separated filenames to import data from")
	err := restoreCmd.Parse(os.Args[2:])
	if err != nil || !restoreCmd.Parsed() {
		restoreCmd.PrintDefaults()
		os.Exit(2)
	}

	if *filenamesStr == "" {
		restoreCmd.PrintDefaults()
		log.Fatalln("The -filenames option is required")
	}

	filenames := strings.Split(*filenamesStr, ",")
	for _, file := range filenames {
		if _, err = os.Stat(file); os.IsNotExist(err) {
			restoreCmd.PrintDefaults()
			log.Fatalf("File path does not exist: %s", file)
		} else if err != nil {
			restoreCmd.PrintDefaults()
			log.Fatalf("Error checking file: %s", file)
		}
	}

	if err := prepareDataDirs(viper.GetString("db_path"), viper.GetString("backup_path")); err != nil {
		log.Fatalf("Couldn't create data directories: %s", err)
	}

	// Restore runs next to the service (docker compose exec), so it only opens
	// the service's ready type DB and never builds or removes type DBs itself.
	restoreTypeDB, err := staticdump.OpenNewestTypeDB(viper.GetString("db_path"))
	if err != nil {
		log.Fatalf("Couldn't open type DB, start the service and wait for its first type DB: %s", err)
	}
	log.Println("TypeDB", restoreTypeDB)

	saver := newRestoreSaver(http.DefaultClient, "http://"+viper.GetString("management_addr"))
	failed := 0
	for _, filename := range filenames {
		saver.counts = restoreCounts{}
		log.Printf("Start restoring: %s", filename)
		err := legacy.RestoreLegacyFile(saver.save, restoreTypeDB, filename)
		if err != nil {
			log.Fatalf("Error while importing legacy file: %s", err)
		}
		log.Printf("Done restoring: %s skipped_existing=%d failed=%d", filename, saver.counts.skipped, saver.counts.failed)
		failed += saver.counts.failed
	}

	if err := restoreTypeDB.Close(); err != nil {
		log.Fatalf("Problem closing typeDB: %s", err)
	}
	if failed > 0 {
		log.Fatalf("Restore finished with failed appraisals: failed=%d", failed)
	}
}

// restoreCounts tallies the outcome of one restored file.
type restoreCounts struct {
	// skipped counts appraisals whose ID the database already holds (409 from /restore).
	skipped int
	// failed counts appraisals refused with any other non-2xx status or lost to a transport error.
	failed int
}

// restoreSaver posts appraisals to the management /restore endpoint.
type restoreSaver struct {
	client  *http.Client
	baseURL string
	counts  restoreCounts
}

func newRestoreSaver(client *http.Client, baseURL string) *restoreSaver {
	return &restoreSaver{client: client, baseURL: baseURL}
}

// save posts one appraisal. A refused row is logged and counted, not returned,
// so the import goes on; a transport error is counted and returned.
func (s *restoreSaver) save(appraisal *evepraisal.Appraisal) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(appraisal); err != nil {
		s.counts.failed++
		return err
	}

	req, err := http.NewRequest("POST", s.baseURL+"/restore", &buf)
	if err != nil {
		s.counts.failed++
		return err
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.counts.failed++
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusConflict:
		s.counts.skipped++
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		s.counts.failed++
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return rerr
		}
		log.Printf("ERROR: %s: %s", resp.Status, string(body))
	}
	return nil
}

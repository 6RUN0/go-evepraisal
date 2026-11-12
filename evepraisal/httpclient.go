package main

import (
	"log"
	"time"

	"github.com/sethgrid/pester"
)

// newStaticdumpHTTPClient returns the client for the SDE manifest and archive.
// It verifies TLS: developers.eveonline.com serves a valid certificate.
func newStaticdumpHTTPClient() *pester.Client {
	client := pester.New()
	client.Concurrency = 1
	// The JSONL archive is about 100 MB.
	client.Timeout = 600 * time.Second
	client.Backoff = pester.ExponentialJitterBackoff
	client.MaxRetries = 2
	client.LogHook = func(e pester.ErrEntry) { log.Println(client.FormatError(e)) }
	return client
}

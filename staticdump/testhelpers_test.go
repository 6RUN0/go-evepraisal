package staticdump

import (
	"time"

	"github.com/sethgrid/pester"
)

// newTestClient returns a pester client that retries once with no delay.
func newTestClient() *pester.Client {
	c := pester.New()
	c.MaxRetries = 1
	c.Backoff = func(int) time.Duration { return 0 }
	return c
}

package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sethgrid/pester"
)

// newTestClient returns a pester client with one attempt and no backoff.
func newTestClient() *pester.Client {
	c := pester.New()
	c.MaxRetries = 1
	c.Backoff = func(int) time.Duration { return 0 }
	return c
}

// newTestFetcher builds a PriceFetcher without starting the background loop.
func newTestFetcher(baseURL string) *PriceFetcher {
	return &PriceFetcher{
		client:  newTestClient(),
		baseURL: baseURL,
		ctx:     context.Background(),
		stop:    make(chan bool),
		wg:      &sync.WaitGroup{},
	}
}

// newOrderServer serves one page of orders per region and an empty page
// after it; any other path fails the test.
func newOrderServer(t *testing.T, ordersByRegion map[int][]MarketOrder) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var region int
		path := strings.TrimSuffix(r.URL.Path, "/")
		if _, err := fmt.Sscanf(path, "/markets/%d/orders", &region); err != nil {
			t.Errorf("unexpected request %s", r.URL)
			return
		}
		orders, ok := ordersByRegion[region]
		if !ok {
			t.Errorf("unexpected request %s", r.URL)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page > 1 {
			orders = nil
		}
		if orders == nil {
			orders = []MarketOrder{}
		}
		w.Header().Set("X-Pages", "1")
		_ = json.NewEncoder(w).Encode(orders)
	}))
	t.Cleanup(srv.Close)
	return srv
}

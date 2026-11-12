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

// stubSleep replaces sleep with a recorder that returns immediately and
// restores the real one on cleanup.
func stubSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var mu sync.Mutex
	pauses := []time.Duration{}
	orig := sleep
	sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		pauses = append(pauses, d)
		return ctx.Err()
	}
	t.Cleanup(func() { sleep = orig })
	return &pauses
}

// step is one scripted server response; check runs when the request arrives.
type step struct {
	status int
	header map[string]string
	body   string
	check  func()
}

// scriptedServer answers requests with steps in order and fails the test on
// an extra request. The returned counter holds the number of requests seen.
func scriptedServer(t *testing.T, steps ...step) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := n
		n++
		mu.Unlock()
		if i >= len(steps) {
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s := steps[i]
		if s.check != nil {
			s.check()
		}
		for k, v := range s.header {
			w.Header().Set(k, v)
		}
		if s.status == 0 {
			s.status = http.StatusOK
		}
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

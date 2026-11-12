package esi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evepraisal/go-evepraisal"
)

func TestFetchOrderDataAggregatesByMarket(t *testing.T) {
	srv := newOrderServer(t, map[int][]MarketOrder{
		10000043: {
			{Type: 34, SystemID: 30003491, Price: 5.0, Volume: 10},
			{Type: 34, SystemID: 30003491, Price: 6.0, Volume: 20},
			{Type: 34, SystemID: 30000001, StationID: 60000001, Price: 4.0, Volume: 5},
			{Type: 34, StationID: 60008950, Price: 3.0, Volume: 7, Buy: true},
		},
	})
	p := newTestFetcher(srv.URL)

	prices, _, err := p.FetchOrderData(p.client, srv.URL, []int{10000043})
	if err != nil {
		t.Fatal(err)
	}

	amarr := prices["amarr"][34]
	if amarr.Sell.Min != 5.0 {
		t.Errorf("amarr Sell.Min = %v, want 5", amarr.Sell.Min)
	}
	if amarr.Sell.Volume != 30 {
		t.Errorf("amarr Sell.Volume = %v, want 30", amarr.Sell.Volume)
	}
	if amarr.Buy.Max != 3.0 {
		t.Errorf("amarr Buy.Max = %v, want 3", amarr.Buy.Max)
	}
	if amarr.Strategy != "orders" {
		t.Errorf("amarr Strategy = %q, want orders", amarr.Strategy)
	}
	if got := prices["universe"][34].Sell.Min; got != 4.0 {
		t.Errorf("universe Sell.Min = %v, want 4", got)
	}
}

func TestFetchOrderDataMultiRegion(t *testing.T) {
	srv := newOrderServer(t, map[int][]MarketOrder{
		10000002: {{Type: 34, SystemID: 30000142, Price: 5.0, Volume: 10}},
		10000043: {{Type: 34, SystemID: 30003491, Price: 6.0, Volume: 20}},
	})
	p := newTestFetcher(srv.URL)

	prices, _, err := p.FetchOrderData(p.client, srv.URL, []int{10000002, 10000043})
	if err != nil {
		t.Fatal(err)
	}

	if got := prices["universe"][34].Sell.Volume; got != 30 {
		t.Errorf("universe Sell.Volume = %v, want 30", got)
	}
	if got := prices["jita"][34].Sell.Volume; got != 10 {
		t.Errorf("jita Sell.Volume = %v, want 10", got)
	}
	if got := prices["amarr"][34].Sell.Volume; got != 20 {
		t.Errorf("amarr Sell.Volume = %v, want 20", got)
	}
}

func TestNewPriceFetcherNormalizesBaseURL(t *testing.T) {
	paths := make(chan string, 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case paths <- r.URL.Path:
		default:
		}
		// a failing server keeps the first cycle away from the nil price db
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p, err := NewPriceFetcher(context.Background(), nil, srv.URL+"/latest", newTestClient(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got := <-paths
	p.Close()

	if !strings.HasPrefix(got, "/markets/") {
		t.Errorf("first request path = %q, want it to start with /markets/", got)
	}
}

// pagedServer serves the given pages of region orders and declares xPages
// ("" omits the header); a request past the served pages fails the test.
func pagedServer(t *testing.T, xPages string, pages ...[]MarketOrder) (*httptest.Server, *[]int) {
	t.Helper()
	var mu sync.Mutex
	requested := []int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		mu.Lock()
		requested = append(requested, page)
		mu.Unlock()
		if page < 1 || page > len(pages) {
			t.Errorf("unexpected request %s", r.URL)
			return
		}
		if xPages != "" {
			w.Header().Set("X-Pages", xPages)
		}
		_ = json.NewEncoder(w).Encode(pages[page-1])
	}))
	t.Cleanup(srv.Close)
	return srv, &requested
}

func sellOrders(n int) []MarketOrder {
	orders := make([]MarketOrder, n)
	for i := range orders {
		orders[i] = MarketOrder{Type: 34, SystemID: 30000142, Price: 5.0, Volume: 1}
	}
	return orders
}

func TestFetchOrderPagesStopAtXPages(t *testing.T) {
	srv, requested := pagedServer(t, "2", sellOrders(2), sellOrders(3))
	p := newTestFetcher(srv.URL)

	prices, _, err := p.FetchOrderData(p.client, srv.URL, []int{10000002})
	if err != nil {
		t.Fatal(err)
	}
	if len(*requested) != 2 || (*requested)[0] != 1 || (*requested)[1] != 2 {
		t.Errorf("requested pages = %v, want [1 2]", *requested)
	}
	if got := prices["universe"][34].Sell.OrderCount; got != 5 {
		t.Errorf("order count = %d, want 5", got)
	}
}

func TestFetchOrderPagesWithoutXPages(t *testing.T) {
	srv, requested := pagedServer(t, "", sellOrders(2))
	p := newTestFetcher(srv.URL)

	if _, _, err := p.FetchOrderData(p.client, srv.URL, []int{10000002}); err != nil {
		t.Fatal(err)
	}
	if len(*requested) != 1 {
		t.Errorf("requested pages = %v, want one", *requested)
	}
}

func TestFetchOrderPagesFirstPage404(t *testing.T) {
	srv, _ := scriptedServer(t, step{status: 404})
	p := newTestFetcher(srv.URL)

	prices, _, err := p.FetchOrderData(p.client, srv.URL, []int{10000002})
	if err != nil {
		t.Fatalf("err = %v, want nil for an empty region", err)
	}
	if len(prices["universe"]) != 0 {
		t.Errorf("universe prices = %v, want none", prices["universe"])
	}
}

// stubPriceDB records UpdatePrices calls and serves a previous universe price.
type stubPriceDB struct {
	mu       sync.Mutex
	updates  []evepraisal.MarketItemPrices
	universe map[int64]evepraisal.Prices
}

func newStubPriceDB() *stubPriceDB {
	return &stubPriceDB{universe: map[int64]evepraisal.Prices{
		34: {Sell: evepraisal.PriceStats{Min: 42, Volume: 100}, Strategy: "orders"},
	}}
}

func (s *stubPriceDB) GetPrice(market string, typeID int64) (evepraisal.Prices, bool) {
	if market != "universe" {
		return evepraisal.Prices{}, false
	}
	p, ok := s.universe[typeID]
	return p, ok
}

func (s *stubPriceDB) UpdatePrices(items []evepraisal.MarketItemPrices) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, items...)
	return nil
}

func (s *stubPriceDB) Close() error { return nil }

// written returns the written prices by market and type.
func (s *stubPriceDB) written() map[string]map[int64]evepraisal.Prices {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]map[int64]evepraisal.Prices{}
	for _, item := range s.updates {
		if out[item.Market] == nil {
			out[item.Market] = map[int64]evepraisal.Prices{}
		}
		out[item.Market][item.TypeID] = item.Prices
	}
	return out
}

// regionFixture is the order book of one region; failPage is answered 500
// (0 for none) and onRequest, when set, runs before every answer.
type regionFixture struct {
	pages     [][]MarketOrder
	failPage  int
	onRequest func(r *http.Request)
}

// defaultRegionFixtures covers all six polled regions. Region 10000002 fails
// on page 2; its page 1 carries an order in an Amarr system that must not leak.
func defaultRegionFixtures() map[int]*regionFixture {
	order := func(system, station int64, price float64) MarketOrder {
		return MarketOrder{Type: 34, SystemID: system, StationID: station, Price: price, Volume: 10}
	}
	return map[int]*regionFixture{
		10000002: {pages: [][]MarketOrder{
			{order(30000142, 0, 4), order(30000144, 0, 4), order(30003491, 0, 1)},
			{order(30000142, 0, 4)},
		}, failPage: 2},
		10000027: {pages: [][]MarketOrder{{order(30000001, 0, 2)}}},
		10000030: {pages: [][]MarketOrder{{order(30002510, 0, 5)}}},
		10000032: {pages: [][]MarketOrder{{order(0, 60011866, 5)}}},
		10000042: {pages: [][]MarketOrder{{order(0, 60005236, 5)}}},
		10000043: {pages: [][]MarketOrder{
			{order(30003491, 0, 6), order(30003491, 0, 7), order(30003491, 0, 8)},
			{order(30003491, 0, 9), order(30003491, 0, 10)},
		}},
	}
}

// newMarketServer serves region orders from fixtures and /markets/prices with
// types 34 and 35 (35 has no orders), or pricesStatus when it is not 200.
// Every /markets/prices request is reported on pricesSeen without blocking.
func newMarketServer(t *testing.T, regions map[int]*regionFixture, pricesStatus int, pricesSeen chan<- struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/markets/prices" {
			if pricesSeen != nil {
				select {
				case pricesSeen <- struct{}{}:
				default:
				}
			}
			if pricesStatus != http.StatusOK {
				w.WriteHeader(pricesStatus)
				return
			}
			_, _ = w.Write([]byte(`[{"type_id":34,"average_price":3,"adjusted_price":3},{"type_id":35,"average_price":8,"adjusted_price":8}]`))
			return
		}
		var region int
		if _, err := fmt.Sscanf(r.URL.Path, "/markets/%d/orders", &region); err != nil {
			t.Errorf("unexpected request %s", r.URL)
			return
		}
		f, ok := regions[region]
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if !ok || page < 1 || page > len(f.pages) {
			t.Errorf("unexpected request %s", r.URL)
			return
		}
		if f.onRequest != nil {
			f.onRequest(r)
		}
		if page == f.failPage {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Pages", strconv.Itoa(len(f.pages)))
		_ = json.NewEncoder(w).Encode(f.pages[page-1])
	}))
	t.Cleanup(srv.Close)
	return srv
}

// captureLog redirects the standard logger into a buffer until cleanup.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

func TestFetchOrderDataRegionFailure(t *testing.T) {
	srv := newMarketServer(t, defaultRegionFixtures(), http.StatusOK, nil)
	p := newTestFetcher(srv.URL)

	prices, failed, err := p.FetchOrderData(p.client, srv.URL, defaultRegionIDs)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	for _, market := range []string{"jita", "perimeter", "universe"} {
		if _, ok := prices[market]; ok {
			t.Errorf("market %s present, want absent after its region failed", market)
		}
	}
	amarr, ok := prices["amarr"]
	if !ok {
		t.Fatal("amarr absent, want present")
	}
	if got := amarr[34].Sell.Min; got != 6 {
		t.Errorf("amarr Sell.Min = %v, want 6 (orders of the failed region leaked in)", got)
	}
	if got := amarr[34].Sell.OrderCount; got != 5 {
		t.Errorf("amarr Sell.OrderCount = %d, want 5", got)
	}
	if len(failed) != 1 || failed[10000002] == nil || !strings.Contains(failed[10000002].Error(), "500") {
		t.Errorf("failed = %v, want only 10000002 with a 500 error", failed)
	}
}

func TestFetchOrderDataUnrequestedMarketsAbsent(t *testing.T) {
	srv := newMarketServer(t, defaultRegionFixtures(), http.StatusOK, nil)
	p := newTestFetcher(srv.URL)

	prices, _, err := p.FetchOrderData(p.client, srv.URL, []int{10000043})
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 2 || prices["amarr"] == nil || prices["universe"] == nil {
		t.Errorf("markets = %v, want only amarr and universe", keysOf(prices))
	}
}

func TestFetchOrderDataSeveralFailuresLeaveNoGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	fixtures := defaultRegionFixtures()
	for _, f := range fixtures {
		f.failPage = 1
	}
	srv := newMarketServer(t, fixtures, http.StatusOK, nil)
	p := newTestFetcher(srv.URL)
	_ = captureLog(t)

	done := make(chan map[int]error, 1)
	go func() {
		_, failed, _ := p.FetchOrderData(p.client, srv.URL, defaultRegionIDs)
		done <- failed
	}()
	select {
	case failed := <-done:
		if len(failed) != len(defaultRegionIDs) {
			t.Errorf("failed regions = %d, want %d", len(failed), len(defaultRegionIDs))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FetchOrderData did not return with several failing regions")
	}

	srv.Close()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("goroutines = %d after FetchOrderData, want <= %d", n, before)
	}
}

func keysOf(m map[string]map[int64]evepraisal.Prices) []string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// newRunOnceFetcher returns a fetcher over the given server with a stub price DB.
func newRunOnceFetcher(srv *httptest.Server) (*PriceFetcher, *stubPriceDB) {
	db := newStubPriceDB()
	p := newTestFetcher(srv.URL)
	p.db = db
	return p, db
}

func TestRunOnceKeepsPricesOfFailedRegion(t *testing.T) {
	srv := newMarketServer(t, defaultRegionFixtures(), http.StatusOK, nil)
	p, db := newRunOnceFetcher(srv)

	p.runOnce()

	written := db.written()
	if got := written["amarr"][34].Sell.Min; got != 6 {
		t.Errorf("amarr Sell.Min = %v, want 6", got)
	}
	for _, market := range []string{"jita", "perimeter", "universe"} {
		if _, ok := written[market]; ok {
			t.Errorf("market %s written, want its previous prices kept", market)
		}
	}
}

func TestRunOnceUsesPreviousUniverseForFallback(t *testing.T) {
	fixtures := defaultRegionFixtures()
	fixtures[10000043].pages = [][]MarketOrder{{{Type: 34, SystemID: 30003491, Price: 6, Volume: 1}}}
	srv := newMarketServer(t, fixtures, http.StatusOK, nil)
	p, db := newRunOnceFetcher(srv)

	p.runOnce()

	got := db.written()["amarr"][34]
	if got.Sell.Min != 42 || got.Strategy != "orders_universe" {
		t.Errorf("amarr type 34 = Sell.Min %v Strategy %q, want 42 orders_universe", got.Sell.Min, got.Strategy)
	}
}

func TestRunOnceAllRegionsFail(t *testing.T) {
	fixtures := defaultRegionFixtures()
	for _, f := range fixtures {
		f.failPage = 1
	}
	srv := newMarketServer(t, fixtures, http.StatusOK, nil)
	p, db := newRunOnceFetcher(srv)

	p.runOnce()

	if n := len(db.written()); n != 0 {
		t.Errorf("markets written = %d, want 0", n)
	}
}

func TestRunOnceCCPPricesFailureStillUpdatesOrders(t *testing.T) {
	srv := newMarketServer(t, defaultRegionFixtures(), http.StatusInternalServerError, nil)
	p, db := newRunOnceFetcher(srv)
	logs := captureLog(t)

	p.runOnce()

	if _, ok := db.written()["amarr"][34]; !ok {
		t.Error("amarr type 34 not written, want order prices without the CCP fallback")
	}
	if !strings.Contains(logs.String(), "CCP") {
		t.Errorf("log = %q, want a CCP error", logs.String())
	}
}

func TestRunOnceStopDuringFetchWritesNothing(t *testing.T) {
	fixtures := defaultRegionFixtures()
	started := make(chan struct{})
	var once sync.Once
	fixtures[10000043].onRequest = func(r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}
	srv := newMarketServer(t, fixtures, http.StatusOK, nil)
	p, db := newRunOnceFetcher(srv)

	done := make(chan struct{})
	go func() {
		p.runOnce()
		close(done)
	}()
	<-started
	close(p.stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runOnce did not return after stop")
	}
	if n := len(db.written()); n != 0 {
		t.Errorf("markets written = %d, want 0 after stop", n)
	}
}

func TestRunOnceSetsUpdatedToFetchStart(t *testing.T) {
	srv := newMarketServer(t, defaultRegionFixtures(), http.StatusOK, nil)
	p, db := newRunOnceFetcher(srv)

	before := time.Now()
	p.runOnce()
	after := time.Now()

	var first time.Time
	for market, prices := range db.written() {
		for typeID, price := range prices {
			if price.Updated.Before(before) || price.Updated.After(after) {
				t.Errorf("%s/%d Updated = %v, want within [%v, %v]", market, typeID, price.Updated, before, after)
			}
			if first.IsZero() {
				first = price.Updated
			} else if !price.Updated.Equal(first) {
				t.Errorf("%s/%d Updated = %v, want the cycle start %v for every price", market, typeID, price.Updated, first)
			}
		}
	}
	if first.IsZero() {
		t.Fatal("nothing written")
	}
}

func TestRunOnceLogsPerRegion(t *testing.T) {
	srv := newMarketServer(t, defaultRegionFixtures(), http.StatusOK, nil)
	p, _ := newRunOnceFetcher(srv)
	logs := captureLog(t)

	p.runOnce()

	re := regexp.MustCompile(`region=(\d+) pages=(\d+) orders=(\d+) duration=\S+ error="(.*)"`)
	matches := re.FindAllStringSubmatch(logs.String(), -1)
	if len(matches) != 6 {
		t.Fatalf("region log lines = %d, want 6; log:\n%s", len(matches), logs.String())
	}
	for _, m := range matches {
		switch m[1] {
		case "10000043":
			if m[2] != "2" || m[3] != "5" || m[4] != "" {
				t.Errorf("10000043 line = %q, want pages=2 orders=5 error=\"\"", m[0])
			}
		case "10000002":
			if !strings.Contains(m[4], "500") {
				t.Errorf("10000002 line = %q, want an error with 500", m[0])
			}
		}
	}
}

func TestNewPriceFetcherUsesInterval(t *testing.T) {
	cycles := make(chan struct{}, 10)
	srv := newMarketServer(t, defaultRegionFixtures(), http.StatusOK, cycles)
	_ = captureLog(t)

	p, err := NewPriceFetcher(context.Background(), newStubPriceDB(), srv.URL, newTestClient(), 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-cycles:
		case <-timeout:
			t.Fatalf("cycles = %d within 5s, want >= 2", i)
		}
	}
	requireReturns(t, "Close", func() { _ = p.Close() })
}

func TestNewPriceFetcherCloseWhileWaiting(t *testing.T) {
	cycles := make(chan struct{}, 10)
	srv := newMarketServer(t, defaultRegionFixtures(), http.StatusOK, cycles)
	_ = captureLog(t)

	p, err := NewPriceFetcher(context.Background(), newStubPriceDB(), srv.URL, newTestClient(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-cycles:
	case <-time.After(5 * time.Second):
		t.Fatal("first cycle did not start")
	}
	requireReturns(t, "Close", func() { _ = p.Close() })
}

// requireReturns fails the test when fn does not return within 5 seconds.
func requireReturns(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return within 5s", name)
	}
}

// pauseServer answers every request with a low rate limit budget and reports
// the first real pause on the returned channel; the counter holds the pauses in progress.
func pauseServer(t *testing.T) (*httptest.Server, <-chan struct{}, *atomic.Int32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ratelimit-Remaining", "1")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)
	paused := make(chan struct{})
	var once sync.Once
	orig := sleep
	active := &atomic.Int32{}
	sleep = func(ctx context.Context, d time.Duration) error {
		active.Add(1)
		defer active.Add(-1)
		once.Do(func() { close(paused) })
		return orig(ctx, d)
	}
	t.Cleanup(func() { sleep = orig })
	return srv, paused, active
}

func TestPriceFetcherCloseDuringRatePause(t *testing.T) {
	srv, paused, active := pauseServer(t)
	_ = captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p, err := NewPriceFetcher(ctx, newStubPriceDB(), srv.URL, newTestClient(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	<-paused
	requireReturns(t, "cancel and Close", func() {
		cancel()
		_ = p.Close()
	})
	requireNoPauses(t, active)
}

func TestPriceFetcherCloseInterruptsPauseWithoutCancel(t *testing.T) {
	srv, paused, active := pauseServer(t)
	_ = captureLog(t)

	p, err := NewPriceFetcher(context.Background(), newStubPriceDB(), srv.URL, newTestClient(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	<-paused
	requireReturns(t, "Close", func() { _ = p.Close() })
	requireNoPauses(t, active)
}

// requireNoPauses fails the test when a worker is still pausing after Close returned.
func requireNoPauses(t *testing.T, active *atomic.Int32) {
	t.Helper()
	if n := active.Load(); n != 0 {
		t.Errorf("pauses in progress after Close = %d, want 0", n)
	}
}

// TestRunOnceContextCancelDuringCCPPricesWritesNothing pins that cancelling
// p.ctx (fetcherCancel runs before Close) while CCP prices load is a stop:
// no markets are written without the CCP fallback and no CCP error is logged.
func TestRunOnceContextCancelDuringCCPPricesWritesNothing(t *testing.T) {
	market := newMarketServer(t, defaultRegionFixtures(), http.StatusOK, nil)
	pricesStarted := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/markets/prices" {
			once.Do(func() { close(pricesStarted) })
			<-r.Context().Done()
			return
		}
		market.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	p, db := newRunOnceFetcher(srv)
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	logs := captureLog(t)

	done := make(chan struct{})
	go func() {
		p.runOnce()
		close(done)
	}()
	select {
	case <-pricesStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("CCP prices were not requested")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runOnce did not return after the context was cancelled")
	}
	if n := len(db.written()); n != 0 {
		t.Errorf("markets written = %d, want 0 after cancel", n)
	}
	if strings.Contains(logs.String(), "CCP") {
		t.Errorf("log = %q, want no CCP error on stop", logs.String())
	}
}

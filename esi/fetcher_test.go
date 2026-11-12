package esi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
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

	prices, err := p.FetchOrderData(p.client, srv.URL, []int{10000043})
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

	prices, err := p.FetchOrderData(p.client, srv.URL, []int{10000002, 10000043})
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

	p, err := NewPriceFetcher(context.Background(), nil, srv.URL+"/latest", newTestClient())
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

	prices, err := p.FetchOrderData(p.client, srv.URL, []int{10000002})
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

	if _, err := p.FetchOrderData(p.client, srv.URL, []int{10000002}); err != nil {
		t.Fatal(err)
	}
	if len(*requested) != 1 {
		t.Errorf("requested pages = %v, want one", *requested)
	}
}

func TestFetchOrderPagesFirstPage404(t *testing.T) {
	srv, _ := scriptedServer(t, step{status: 404})
	p := newTestFetcher(srv.URL)

	prices, err := p.FetchOrderData(p.client, srv.URL, []int{10000002})
	if err != nil {
		t.Fatalf("err = %v, want nil for an empty region", err)
	}
	if len(prices["universe"]) != 0 {
		t.Errorf("universe prices = %v, want none", prices["universe"])
	}
}

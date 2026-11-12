package esi

import "testing"

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

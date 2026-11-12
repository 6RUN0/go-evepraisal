package esi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/evepraisal/go-evepraisal"
	"github.com/sethgrid/pester"
)

// MarketOrder represents a market order in ESI
type MarketOrder struct {
	ID            int64   `json:"order_id"`
	Type          int64   `json:"type_id"`
	StationID     int64   `json:"location_id"`
	SystemID      int64   `json:"system_id"`
	Volume        int64   `json:"volume_remain"`
	MinVolume     int64   `json:"min_volume"`
	Price         float64 `json:"price"`
	Buy           bool    `json:"is_buy_order"`
	Duration      int64   `json:"duration"`
	Issued        string  `json:"issued"`
	VolumeEntered int64   `json:"volumeEntered"`
	Range         string  `json:"range"`
}

// SpecialRegions defines which markets we care about and the region each one lives in
var SpecialRegions = []struct {
	name     string
	regionID int
	stations []int64
	systems  []int64
}{
	{
		name:     "jita",
		regionID: 10000002,
		systems:  []int64{30000142},
	}, {
		name:     "perimeter",
		regionID: 10000002,
		systems:  []int64{30000144},
	}, {
		name:     "amarr",
		regionID: 10000043,
		stations: []int64{60008950, 60002569, 60008494},
		systems:  []int64{30003491},
	}, {
		name:     "dodixie",
		regionID: 10000032,
		stations: []int64{60011866, 60001867},
		systems:  []int64{30002661},
	}, {
		name:     "hek",
		regionID: 10000042,
		stations: []int64{60005236, 60004516, 60015140, 60005686, 60011287, 60005236},
	}, {
		name:     "rens",
		regionID: 10000030,
		systems:  []int64{30002510, 30002526},
	},
}

// defaultRegionIDs are the regions polled every cycle.
// 10000027 has no market of its own and feeds "universe" only.
var defaultRegionIDs = []int{10000002, 10000027, 10000030, 10000032, 10000042, 10000043}

// PriceFetcher fetches prices and populates the given priceDB
type PriceFetcher struct {
	db        evepraisal.PriceDB
	client    *pester.Client
	baseURL   string
	regionIDs []int

	ctx  context.Context
	stop chan bool
	wg   *sync.WaitGroup
}

// NewPriceFetcher returns a new PriceFetcher that refreshes prices every interval
func NewPriceFetcher(ctx context.Context, priceDB evepraisal.PriceDB, baseURL string, client *pester.Client, interval time.Duration) (*PriceFetcher, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("price fetch interval must be positive, got %s", interval)
	}

	p := &PriceFetcher{
		db:        priceDB,
		client:    client,
		baseURL:   NormalizeBaseURL(baseURL),
		regionIDs: defaultRegionIDs,

		ctx:  ctx,
		stop: make(chan bool),
		wg:   &sync.WaitGroup{},
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			start := time.Now()
			p.runOnce()
			select {
			case <-time.After(interval - time.Since(start)):
			case <-p.stop:
				return
			}
		}
	}()

	return p, nil
}

// Close should be called to stop the fetcher worker(s)
func (p *PriceFetcher) Close() error {
	close(p.stop)
	p.wg.Wait()
	return nil
}

// stopContext returns a context that is cancelled by p.ctx or by Close,
// so requests and rate limit pauses end on either.
func (p *PriceFetcher) stopContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(p.ctx)
	go func() {
		select {
		case <-p.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (p *PriceFetcher) runOnce() {
	start := time.Now()
	log.Println("Fetch market data")
	priceMap, failed, err := p.FetchOrderData(p.client, p.baseURL, p.regionIDs)
	if err != nil {
		log.Println("ERROR: fetching market data: ", err)
		return
	}
	if len(failed) > 0 {
		log.Printf("ERROR: %d of %d regions failed, keeping the previous prices of their markets and universe", len(failed), len(p.regionIDs))
	}

	pricesFromCCP, err := p.FetchPriceData(p.client, p.baseURL)
	if err != nil {
		log.Println("ERROR: fetching CCP price data, skipping the CCP price fallback: ", err)
	}

	// Without a fresh universe the fallback uses the stored one; it went through
	// the CCP fallback in its own cycle, like a fresh one does below.
	universe, haveUniverse := priceMap["universe"]
	previousUniverse := make(map[int64]evepraisal.Prices)
	universePrice := func(typeID int64) (evepraisal.Prices, bool) {
		if haveUniverse {
			price, ok := universe[typeID]
			return price, ok
		}
		if price, ok := previousUniverse[typeID]; ok {
			return price, true
		}
		price, ok := p.db.GetPrice("universe", typeID)
		if ok {
			previousUniverse[typeID] = price
		}
		return price, ok
	}

	// universe goes first so the other markets fall back on it after its CCP fallback
	markets := make([]string, 0, len(priceMap))
	if haveUniverse {
		markets = append(markets, "universe")
	}
	for market := range priceMap {
		if market != "universe" {
			markets = append(markets, market)
		}
	}

	for _, market := range markets {
		marketPrices := priceMap[market]
		// Use CCP's price if our regional price is too low
		for typeID, prices := range pricesFromCCP {
			p, ok := marketPrices[typeID]
			if !ok || p.Sell.Volume < 10 {
				marketPrices[typeID] = prices
			}
		}

		// Use the universe price if our regional price is too low (override CCP's price)
		for typeID, p := range marketPrices {
			if p.Sell.Volume < 2 {
				fallback, ok := universePrice(typeID)
				if ok && fallback.Sell.Volume >= 2 {
					fallback.Strategy = "orders_universe"
					marketPrices[typeID] = fallback
				}
			}

			if market != "universe" && p.Buy.Volume > 0 && p.Sell.Volume > 0 && p.Buy.Max > p.Sell.Min {
				delta := p.Buy.Max - p.Sell.Min
				if delta > 1000000 {
					log.Printf("MARKET: Prices are wack for %d in %s", typeID, market)
				}
			}
		}
	}

	for _, market := range markets {
		// this takes awhile, so let's check to see if we should stop between markets
		select {
		case <-p.stop:
			return
		default:
		}

		pmap := priceMap[market]
		items := make([]evepraisal.MarketItemPrices, 0, len(pmap))
		for typeID, prices := range pmap {
			prices.Updated = start
			items = append(items, evepraisal.MarketItemPrices{
				Market: market,
				TypeID: typeID,
				Prices: prices,
			})
		}

		err = p.db.UpdatePrices(items)
		if err != nil {
			log.Printf("Error when updating prices: %s", err)
		}
	}
	log.Println("Done fetching market data")
}

// FetchPriceData fetches CCP's pricing information for every type
func (p *PriceFetcher) FetchPriceData(client *pester.Client, baseURL string) (map[int64]evepraisal.Prices, error) {
	start := time.Now()
	ctx, cancel := p.stopContext()
	defer cancel()
	url := fmt.Sprintf("%s/markets/prices", baseURL)
	esiPrices := make([]struct {
		TypeID        int64   `json:"type_id"`
		AveragePrice  float64 `json:"average_price"`
		AdjustedPrice float64 `json:"adjusted_price"`
	}, 0)
	_, err := fetchURL(ctx, client, url, &esiPrices)
	if err != nil {
		return nil, err
	}

	allPrices := make(map[int64]evepraisal.Prices, len(esiPrices))
	for _, p := range esiPrices {
		priceToUse := p.AveragePrice
		if priceToUse == 0 {
			priceToUse = p.AdjustedPrice
		}
		stats := evepraisal.PriceStats{
			Average:    p.AveragePrice,
			Max:        priceToUse,
			Median:     priceToUse,
			Min:        priceToUse,
			Percentile: p.AdjustedPrice,
		}
		allPrices[p.TypeID] = evepraisal.Prices{
			All:      stats,
			Buy:      stats,
			Sell:     stats,
			Updated:  start,
			Strategy: "ccp",
		}
	}
	return allPrices, nil
}

// fetchRegionOrders fetches every page of one region; on error the orders
// fetched so far are returned with it and must not be used.
func fetchRegionOrders(ctx context.Context, client *pester.Client, baseURL string, regionID int) ([]MarketOrder, int, error) {
	var all []MarketOrder
	pages := 1
	fetched := 0
	for page := 1; page <= pages; page++ {
		if err := ctx.Err(); err != nil {
			return all, fetched, err
		}
		url := fmt.Sprintf("%s/markets/%d/orders?order_type=all&page=%d", baseURL, regionID, page)
		var orders []MarketOrder
		info, err := fetchURL(ctx, client, url, &orders)
		if err != nil {
			return all, fetched, fmt.Errorf("Failed to fetch market orders: %s (%s)", err, url)
		}
		fetched++
		all = append(all, orders...)
		if page == 1 {
			pages = info.Pages
		}
	}
	return all, fetched, nil
}

// FetchOrderData concurrently fetches from each region that we care about.
// prices holds only markets whose region was requested and succeeded; "universe"
// only when all requested regions succeeded. failed maps each failed region to its error.
// err is set when the fetch was stopped or its context cancelled.
func (p *PriceFetcher) FetchOrderData(client *pester.Client, baseURL string, regionIDs []int) (prices map[string]map[int64]evepraisal.Prices, failed map[int]error, err error) {
	fetchStart := time.Now()
	ctx, cancel := p.stopContext()
	defer cancel()

	type regionResult struct {
		orders []MarketOrder
		err    error
	}
	results := make([]regionResult, len(regionIDs))
	wg := &sync.WaitGroup{}
	for i, regionID := range regionIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			regionStart := time.Now()
			orders, pages, err := fetchRegionOrders(ctx, client, baseURL, regionID)
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			log.Printf("region=%d pages=%d orders=%d duration=%s error=%q", regionID, pages, len(orders), time.Since(regionStart).Round(time.Millisecond), errText)
			results[i] = regionResult{orders: orders, err: err}
		}()
	}
	// Workers end on their own: every request and pause watches ctx, which Close cancels.
	wg.Wait()

	select {
	case <-p.stop:
		return nil, nil, errors.New("Stopping during price fetch")
	default:
	}
	if err := p.ctx.Err(); err != nil {
		return nil, nil, err
	}

	failed = make(map[int]error)
	succeeded := make(map[int]bool)
	allOrdersByType := make(map[int64][]MarketOrder)
	for i, regionID := range regionIDs {
		if results[i].err != nil {
			failed[regionID] = results[i].err
			continue
		}
		succeeded[regionID] = true
		for _, order := range results[i].orders {
			allOrdersByType[order.Type] = append(allOrdersByType[order.Type], order)
		}
	}

	log.Println("Performing aggregates on order data")
	// Calculate aggregates that we care about:
	newPriceMap := make(map[string]map[int64]evepraisal.Prices)
	for _, region := range SpecialRegions {
		if succeeded[region.regionID] {
			newPriceMap[region.name] = make(map[int64]evepraisal.Prices)
		}
	}
	haveUniverse := len(failed) == 0
	if haveUniverse {
		newPriceMap["universe"] = make(map[int64]evepraisal.Prices)
	}
	for k, orders := range allOrdersByType {
		for _, region := range SpecialRegions {
			if !succeeded[region.regionID] {
				continue
			}
			filteredOrders := make([]MarketOrder, 0)
			for _, order := range orders {
				matched := false
				for _, station := range region.stations {
					if station == order.StationID {
						matched = true
						break
					}
				}
				for _, system := range region.systems {
					if system == order.SystemID {
						matched = true
						break
					}
				}
				if matched {
					filteredOrders = append(filteredOrders, order)
				}
			}
			agg := getPriceAggregatesForOrders(filteredOrders)
			agg.Updated = fetchStart
			agg.Strategy = "orders"
			newPriceMap[region.name][k] = agg
		}
		if haveUniverse {
			agg := getPriceAggregatesForOrders(orders)
			agg.Updated = fetchStart
			newPriceMap["universe"][k] = agg
		}
	}

	log.Println("Finished performing aggregates on order data")

	return newPriceMap, failed, nil
}

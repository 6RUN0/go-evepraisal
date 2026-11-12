package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/evepraisal/go-evepraisal"
	"github.com/sethgrid/pester"
)

const (
	// maxRateLimitAttempts is the total number of requests for one URL when ESI answers 420 or 429.
	maxRateLimitAttempts = 3
	// defaultRetryPause is used when a 420/429 carries no usable header.
	defaultRetryPause = time.Minute
	// maxRetryPause caps every pause so a bad header cannot stall a fetch cycle for hours.
	maxRetryPause = 5 * time.Minute
	// errorBudgetFloor is the X-Esi-Error-Limit-Remain below which the client waits for the window reset.
	errorBudgetFloor = 10
	// rateBudgetFloor is the X-Ratelimit-Remaining below which the client pauses. A 2xx costs 2 tokens
	// (x-ratelimit-used: 2), so 100 tokens are about 50 requests; polling six regions takes about
	// 1000 pages. Re-measure: sum x-pages over `curl -sI 'https://esi.evetech.net/markets/<id>/orders?order_type=all&page=1'`.
	rateBudgetFloor = 100
	// rateBudgetPause is the wait when the rate limit budget is low.
	rateBudgetPause = time.Minute
)

// pageInfo describes the pagination of a response.
type pageInfo struct {
	// Pages is the X-Pages header value, 1 when absent or not a positive integer.
	Pages int
}

// sleep waits for d or until ctx is done; it is a variable so tests can skip real waits.
var sleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func headerSeconds(h http.Header, name string) (time.Duration, bool) {
	n, err := strconv.Atoi(h.Get(name))
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}

func capPause(d time.Duration) time.Duration {
	if d > maxRetryPause {
		return maxRetryPause
	}
	return d
}

// retryPause returns how long to wait before repeating a request answered 420 or 429.
func retryPause(resp *http.Response) time.Duration {
	if d, ok := headerSeconds(resp.Header, "Retry-After"); ok {
		return capPause(d)
	}
	if t, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
		return capPause(max(time.Until(t), 0))
	}
	if resp.StatusCode == 420 {
		if d, ok := headerSeconds(resp.Header, "X-Esi-Error-Limit-Reset"); ok {
			return capPause(d)
		}
	}
	return defaultRetryPause
}

// budgetPause returns how long to wait after a successful response when
// ESI reports a nearly exhausted error or rate limit budget.
func budgetPause(h http.Header) time.Duration {
	// X-From-Cache is deliberately not an exception: httpcache also sets it on a 304
	// revalidation that carries fresh limits. A stale pause after a pure cache hit costs
	// at most rateBudgetPause, a skipped fresh limit can exhaust the budget.
	var pause time.Duration
	if remain, err := strconv.Atoi(h.Get("X-Esi-Error-Limit-Remain")); err == nil && remain < errorBudgetFloor {
		if d, ok := headerSeconds(h, "X-Esi-Error-Limit-Reset"); ok {
			pause = d
		}
	}
	if remaining, err := strconv.Atoi(h.Get("X-Ratelimit-Remaining")); err == nil && remaining < rateBudgetFloor {
		pause = max(pause, rateBudgetPause)
	}
	return capPause(pause)
}

func pagesOf(h http.Header) int {
	if n, err := strconv.Atoi(h.Get("X-Pages")); err == nil && n > 0 {
		return n
	}
	return 1
}

func fetchURL(ctx context.Context, client *pester.Client, url string, r interface{}) (pageInfo, error) {
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return pageInfo{}, err
		}
		req.Header.Set("User-Agent", evepraisal.UserAgent)
		req.Header.Set("X-Compatibility-Date", CompatibilityDate)

		resp, err := client.Do(req.WithContext(ctx))
		if err != nil {
			return pageInfo{}, err
		}

		switch resp.StatusCode {
		case http.StatusOK:
			err = json.NewDecoder(resp.Body).Decode(r)
			resp.Body.Close()
			if err != nil {
				return pageInfo{}, err
			}
			if pause := budgetPause(resp.Header); pause > 0 {
				if err := sleep(ctx, pause); err != nil {
					return pageInfo{}, err
				}
			}
			return pageInfo{Pages: pagesOf(resp.Header)}, nil
		case http.StatusNotFound:
			resp.Body.Close()
			return pageInfo{Pages: 1}, nil
		case 420, http.StatusTooManyRequests:
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if attempt >= maxRateLimitAttempts {
				return pageInfo{}, fmt.Errorf("Error talking to esi: %s", resp.Status)
			}
			if err := sleep(ctx, retryPause(resp)); err != nil {
				return pageInfo{}, err
			}
		default:
			resp.Body.Close()
			return pageInfo{}, fmt.Errorf("Error talking to esi: %s", resp.Status)
		}
	}
}

package esi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evepraisal/go-evepraisal"
)

func fetchOnce(t *testing.T, url string) (pageInfo, error) {
	t.Helper()
	var out []MarketOrder
	return fetchURL(context.Background(), newTestClient(), url, &out)
}

func requireSleeps(t *testing.T, got *[]time.Duration, want ...time.Duration) {
	t.Helper()
	if len(*got) != len(want) {
		t.Fatalf("sleeps = %v, want %v", *got, want)
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Fatalf("sleeps = %v, want %v", *got, want)
		}
	}
}

func TestFetchRequestShape(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		if got := r.Header.Get("X-Compatibility-Date"); got != "2020-01-01" {
			t.Errorf("X-Compatibility-Date = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != evepraisal.UserAgent {
			t.Errorf("User-Agent = %q, want %q", got, evepraisal.UserAgent)
		}
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)
	p := newTestFetcher(srv.URL)

	if _, err := p.FetchOrderData(p.client, srv.URL, []int{10000002}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.FetchPriceData(p.client, srv.URL); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"GET /markets/10000002/orders?order_type=all&page=1",
		"GET /markets/prices",
	}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %v, want %v", seen, want)
	}
}

func TestFetchReadsXPages(t *testing.T) {
	cases := []struct {
		header string
		want   int
	}{{"3", 3}, {"", 1}, {"abc", 1}, {"0", 1}}
	for _, c := range cases {
		hdr := map[string]string{}
		if c.header != "" {
			hdr["X-Pages"] = c.header
		}
		srv, _ := scriptedServer(t, step{header: hdr, body: "[]"})
		info, err := fetchOnce(t, srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		if info.Pages != c.want {
			t.Errorf("X-Pages %q: Pages = %d, want %d", c.header, info.Pages, c.want)
		}
	}
}

func TestFetchRetryAfter429SleepsBeforeRetry(t *testing.T) {
	pauses := stubSleep(t)
	srv, n := scriptedServer(t,
		step{status: 429, header: map[string]string{"Retry-After": "2"}},
		step{body: "[]", check: func() {
			if len(*pauses) != 1 || (*pauses)[0] != 2*time.Second {
				t.Errorf("pauses at retry time = %v, want [2s]", *pauses)
			}
		}},
	)
	if _, err := fetchOnce(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	if *n != 2 {
		t.Errorf("requests = %d, want 2", *n)
	}
}

func TestFetchRetryAfterMissingHeader(t *testing.T) {
	pauses := stubSleep(t)
	srv, _ := scriptedServer(t, step{status: 429}, step{body: "[]"})
	if _, err := fetchOnce(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	requireSleeps(t, pauses, defaultRetryPause)
}

func TestFetchRetryAfterHTTPDate(t *testing.T) {
	pauses := stubSleep(t)
	date := time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)
	srv, _ := scriptedServer(t,
		step{status: 429, header: map[string]string{"Retry-After": date}},
		step{body: "[]"},
	)
	if _, err := fetchOnce(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	if len(*pauses) != 1 {
		t.Fatalf("sleeps = %v, want one", *pauses)
	}
	if d := (*pauses)[0]; d < 2*time.Second || d > 4*time.Second {
		t.Errorf("pause = %v, want about 3s", d)
	}
}

func TestFetchRetryAfterIsCapped(t *testing.T) {
	pauses := stubSleep(t)
	srv, _ := scriptedServer(t,
		step{status: 429, header: map[string]string{"Retry-After": "86400"}},
		step{body: "[]"},
	)
	if _, err := fetchOnce(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	requireSleeps(t, pauses, maxRetryPause)
}

func TestFetchErrorLimit420(t *testing.T) {
	pauses := stubSleep(t)
	srv, n := scriptedServer(t,
		step{status: 420, header: map[string]string{"X-Esi-Error-Limit-Reset": "7"}},
		step{body: "[]"},
	)
	if _, err := fetchOnce(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	requireSleeps(t, pauses, 7*time.Second)
	if *n != 2 {
		t.Errorf("requests = %d, want 2", *n)
	}
}

func TestFetchRetryCap(t *testing.T) {
	pauses := stubSleep(t)
	h := map[string]string{"Retry-After": "1"}
	srv, n := scriptedServer(t, step{status: 429, header: h}, step{status: 429, header: h}, step{status: 429, header: h})
	_, err := fetchOnce(t, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want one mentioning 429", err)
	}
	if *n != 3 {
		t.Errorf("requests = %d, want 3", *n)
	}
	requireSleeps(t, pauses, time.Second, time.Second)
}

func TestFetchLowErrorBudget(t *testing.T) {
	cases := []struct {
		remain string
		want   []time.Duration
	}{{"9", []time.Duration{12 * time.Second}}, {"10", nil}}
	for _, c := range cases {
		pauses := stubSleep(t)
		srv, _ := scriptedServer(t, step{body: "[]", header: map[string]string{
			"X-Esi-Error-Limit-Remain": c.remain,
			"X-Esi-Error-Limit-Reset":  "12",
		}})
		if _, err := fetchOnce(t, srv.URL); err != nil {
			t.Fatal(err)
		}
		requireSleeps(t, pauses, c.want...)
	}
}

func TestFetchLowRateBudget(t *testing.T) {
	cases := []struct {
		remaining string
		want      []time.Duration
	}{{"99", []time.Duration{time.Minute}}, {"100", nil}, {"abc", nil}}
	for _, c := range cases {
		pauses := stubSleep(t)
		srv, _ := scriptedServer(t, step{body: "[]", header: map[string]string{"X-Ratelimit-Remaining": c.remaining}})
		if _, err := fetchOnce(t, srv.URL); err != nil {
			t.Fatal(err)
		}
		requireSleeps(t, pauses, c.want...)
	}
}

func TestFetchLowBothBudgets(t *testing.T) {
	pauses := stubSleep(t)
	srv, _ := scriptedServer(t, step{body: "[]", header: map[string]string{
		"X-Esi-Error-Limit-Remain": "5",
		"X-Esi-Error-Limit-Reset":  "9",
		"X-Ratelimit-Remaining":    "50",
	}})
	if _, err := fetchOnce(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	requireSleeps(t, pauses, time.Minute)
}

func TestFetchNoRateHeadersNoSleep(t *testing.T) {
	pauses := stubSleep(t)
	srv, _ := scriptedServer(t, step{body: "[]"})
	if _, err := fetchOnce(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	requireSleeps(t, pauses)
}

func TestFetchCachedResponseSkipsBudget(t *testing.T) {
	pauses := stubSleep(t)
	srv, _ := scriptedServer(t, step{body: "[]", header: map[string]string{
		"X-From-Cache":          "1",
		"X-Ratelimit-Remaining": "1",
	}})
	if _, err := fetchOnce(t, srv.URL); err != nil {
		t.Fatal(err)
	}
	requireSleeps(t, pauses)
}

func TestFetchPauseIsCancelledByContext(t *testing.T) {
	srv, _ := scriptedServer(t, step{status: 429, header: map[string]string{"Retry-After": "3600"}})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan error, 1)
	go func() {
		var out []MarketOrder
		_, err := fetchURL(ctx, newTestClient(), srv.URL, &out)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pause was not interrupted by cancellation")
	}
}

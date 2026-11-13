package scripts_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

const siteName = "Zima Price"

type pages struct {
	about string
	item  string
}

func healthyPages() pages {
	return pages{
		about: "<h1>About " + siteName + "</h1>",
		item:  "<p>The market price for this item was last updated 5 minutes ago.</p>",
	}
}

func newInstance(t *testing.T, p pages) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\n"))
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/favicon.ico", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("/static/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("icon"))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(p.about))
	})
	mux.HandleFunc("/item/34", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(p.item))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL)
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runVerify(t *testing.T, args ...string) (int, string) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not found")
	}
	cmd := exec.Command("sh", append([]string{"verify-instance.sh"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("run script: %v\n%s", err, out)
	return -1, ""
}

func TestVerifyInstanceAllPass(t *testing.T) {
	srv := newInstance(t, healthyPages())
	code, out := runVerify(t, srv.URL, siteName, "30")
	if code != 0 {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

func TestVerifyInstanceStalePrices(t *testing.T) {
	p := healthyPages()
	p.item = "<p>last updated 2 hours ago.</p>"
	srv := newInstance(t, p)
	code, out := runVerify(t, srv.URL, siteName, "30")
	if code != 1 || !strings.Contains(out, "prices-fresh") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

func TestVerifyInstanceSkipsPricesWithoutInterval(t *testing.T) {
	p := healthyPages()
	p.item = "<p>last updated 2 hours ago.</p>"
	srv := newInstance(t, p)
	code, out := runVerify(t, srv.URL, siteName)
	if code != 0 {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

func TestVerifyInstanceStatusLinkPresent(t *testing.T) {
	p := healthyPages()
	p.about += `<a href="https://status.evepraisal.com">status</a>`
	srv := newInstance(t, p)
	code, out := runVerify(t, srv.URL, siteName, "30")
	if code != 1 || !strings.Contains(out, "no-status-link") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

func TestVerifyInstanceWrongBranding(t *testing.T) {
	p := healthyPages()
	p.about = "<h1>About something else</h1>"
	srv := newInstance(t, p)
	code, out := runVerify(t, srv.URL, siteName, "30")
	if code != 1 || !strings.Contains(out, "branding") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

func TestVerifyInstanceUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	code, out := runVerify(t, "http://"+addr, siteName, "30")
	if code != 1 {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
}

// TestVerifyInstancePricesFreshBoundaries pins the prices-fresh limit of
// interval + 10 minutes (40 at interval 30) and the units the script accepts.
func TestVerifyInstancePricesFreshBoundaries(t *testing.T) {
	cases := []struct {
		name string
		item string
		isOk bool
	}{
		{"at limit", "<p>last updated 40 minutes ago.</p>", true},
		{"past limit", "<p>last updated 41 minutes ago.</p>", false},
		{"now", "<p>last updated now.</p>", true},
		{"seconds", "<p>last updated 5 seconds ago.</p>", true},
		{"days", "<p>last updated 3 days ago.</p>", false},
		{"no stamp", "<p>no price data</p>", false},
		{"one of two stale", "<p>updated 5 minutes ago</p><p>updated 41 minutes ago</p>", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := healthyPages()
			p.item = tc.item
			srv := newInstance(t, p)
			code, out := runVerify(t, srv.URL, siteName, "30")
			if tc.isOk {
				if code != 0 || !strings.Contains(out, "ok   prices-fresh") {
					t.Fatalf("exit %d, want 0 with prices-fresh ok, output:\n%s", code, out)
				}
				return
			}
			if code != 1 || !strings.Contains(out, "FAIL prices-fresh") || !strings.Contains(out, "failed: prices-fresh") {
				t.Fatalf("exit %d, want 1 with only prices-fresh failed, output:\n%s", code, out)
			}
		})
	}
}

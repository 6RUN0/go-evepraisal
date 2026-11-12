package staticdump

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evepraisal/go-evepraisal"
)

const manifestPath = "/static-data/tranquility/latest.jsonl"

func manifestServer(t *testing.T, status int, body string, onRequest func(*http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != manifestPath {
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
			return
		}
		if onRequest != nil {
			onRequest(r)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFindLatestStaticDump(t *testing.T) {
	srv := manifestServer(t, 200, `{"_key": "sde", "buildNumber": 3687575, "releaseDate": "2026-11-11T11:07:08Z"}`+"\n", nil)
	got, err := FindLatestStaticDump(newTestClient(), srv.URL+manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.BuildNumber != 3687575 {
		t.Errorf("BuildNumber = %d", got.BuildNumber)
	}
	want := srv.URL + "/static-data/tranquility/eve-online-static-data-3687575-jsonl.zip"
	if got.URL != want {
		t.Errorf("URL = %q, want %q", got.URL, want)
	}
}

func TestFindLatestStaticDumpSkipsOtherKeys(t *testing.T) {
	srv := manifestServer(t, 200, "{\"_key\": \"other\"}\n\n{\"_key\": \"sde\", \"buildNumber\": 7}\n", nil)
	got, err := FindLatestStaticDump(newTestClient(), srv.URL+manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.BuildNumber != 7 {
		t.Errorf("BuildNumber = %d", got.BuildNumber)
	}
}

func TestFindLatestStaticDumpFirstSDEWins(t *testing.T) {
	srv := manifestServer(t, 200, "{\"_key\": \"sde\", \"buildNumber\": 1}\n{\"_key\": \"sde\", \"buildNumber\": 2}\n", nil)
	got, err := FindLatestStaticDump(newTestClient(), srv.URL+manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.BuildNumber != 1 {
		t.Errorf("BuildNumber = %d, want 1", got.BuildNumber)
	}
}

func TestFindLatestStaticDumpBadLine(t *testing.T) {
	srv := manifestServer(t, 200, "{\"_key\": \"other\"}\n{\n{\"_key\": \"sde\", \"buildNumber\": 1}\n", nil)
	_, err := FindLatestStaticDump(newTestClient(), srv.URL+manifestPath)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("err = %v, want mention of line 2", err)
	}
}

func TestFindLatestStaticDumpNon2xx(t *testing.T) {
	srv := manifestServer(t, 503, "", nil)
	_, err := FindLatestStaticDump(newTestClient(), srv.URL+manifestPath)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v, want mention of 503", err)
	}
}

func TestFindLatestStaticDumpNoSDE(t *testing.T) {
	srv := manifestServer(t, 200, "{\"_key\": \"other\"}\n", nil)
	if _, err := FindLatestStaticDump(newTestClient(), srv.URL+manifestPath); err == nil {
		t.Error("expected error")
	}
}

func TestFindLatestStaticDumpBadBuild(t *testing.T) {
	for _, build := range []string{`"buildNumber": 0`, `"buildNumber": -1`, `"buildNumber": "abc"`, `"buildNumber": 1.5`, `"x": 1`} {
		srv := manifestServer(t, 200, `{"_key": "sde", `+build+"}\n", nil)
		if _, err := FindLatestStaticDump(newTestClient(), srv.URL+manifestPath); err == nil {
			t.Errorf("%s: expected error", build)
		}
	}
}

func TestFindLatestStaticDumpSendsUserAgent(t *testing.T) {
	var ua string
	srv := manifestServer(t, 200, `{"_key": "sde", "buildNumber": 1}`+"\n", func(r *http.Request) { ua = r.Header.Get("User-Agent") })
	if _, err := FindLatestStaticDump(newTestClient(), srv.URL+manifestPath); err != nil {
		t.Fatal(err)
	}
	if ua != evepraisal.UserAgent || !strings.Contains(ua, "go-evepraisal") {
		t.Errorf("User-Agent = %q", ua)
	}
}

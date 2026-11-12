package web

import (
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evepraisal/go-evepraisal"
	"github.com/evepraisal/go-evepraisal/typedb"
	"github.com/gorilla/sessions"
)

func newBrandingContext(t *testing.T) *Context {
	t.Helper()
	ctx := NewContext(&evepraisal.App{})
	ctx.CookieStore = sessions.NewCookieStore(make([]byte, 32))
	if err := ctx.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	return ctx
}

// get requests path through the full HTTP handler and returns status and body.
func get(t *testing.T, ctx *Context, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return rec.Code, string(body)
}

func TestDefaultBrandingMatchesPreviousOutput(t *testing.T) {
	ctx := newBrandingContext(t)
	code, body := get(t, ctx, "/about")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{
		`<a class="navbar-brand" href="/">Evepraisal</a>`,
		`<title>Evepraisal - `,
		`<a href="https://github.com/evepraisal/go-evepraisal" target="_blank">Source on github</a>`,
		`<li>Send an in-game mail to: <a href="https://gate.eveonline.com/Profile/Magerawr" target="_blank">Magerawr</a></li>`,
		`<li>Open an <a href="https://github.com/evepraisal/go-evepraisal/issues?state=open" target="_blank">issue on github</a></li>`,
		`<li>Talk to <a href="https://tweetfleet.slack.com/messages/@sudorandom" target="_blank">@sudorandom</a> in the <a href="https://tweetfleet.slack.com/messages/C03CGPX82" target="_blank">#devfleet channel on the tweetfleet slack</a></li>`,
		`<li>Mention <a href="https://twitter.com/sudorandom" target="_blank">@sudorandom</a> on twitter.</a></li>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestSiteNameInTitleAndNavbar(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.SiteName = "Zima Corp Evepraisal"
	_, body := get(t, ctx, "/about")
	if !strings.Contains(body, `<a class="navbar-brand" href="/">Zima Corp Evepraisal</a>`) {
		t.Errorf("navbar lacks site name")
	}
	if !strings.Contains(body, "<title>Zima Corp Evepraisal - ") {
		t.Errorf("title lacks site name")
	}
}

func TestSiteNameIsHTMLEscaped(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.SiteName = "A&B <i>"
	_, body := get(t, ctx, "/about")
	if !strings.Contains(body, "A&amp;B &lt;i&gt;") {
		t.Errorf("site name not escaped")
	}
	if strings.Contains(body, "<i>") {
		t.Errorf("raw <i> in output")
	}
}

func TestFooterHasNoStatusOrBlogLink(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.SourceURL = "https://example.org/zima/evepraisal"
	_, body := get(t, ctx, "/about")
	for _, bad := range []string{"status.evepraisal.com", "sudorandom.com"} {
		if strings.Contains(body, bad) {
			t.Errorf("body contains %q", bad)
		}
	}
	for _, want := range []string{`href="https://example.org/zima/evepraisal"`, `href="https://example.org/zima/evepraisal/issues"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestAboutRendersContactsHTML(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.AboutContactsHTML = template.HTML("<li>mail: X</li>")
	_, body := get(t, ctx, "/about")
	if !strings.Contains(body, "<li>mail: X</li>") {
		t.Errorf("contacts HTML not rendered verbatim")
	}
	if strings.Contains(body, "Magerawr") {
		t.Errorf("default contacts still present")
	}
}

func TestLayoutIconLinkPointsToFaviconRoute(t *testing.T) {
	ctx := newBrandingContext(t)
	_, body := get(t, ctx, "/about")
	if !strings.Contains(body, `<link rel="icon" href="/favicon.ico"`) {
		t.Errorf("icon link does not point to /favicon.ico")
	}
}

func TestFaviconFromConfiguredPath(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.FaviconPath = filepath.Join(t.TempDir(), "my.ico")
	if err := os.WriteFile(ctx.FaviconPath, []byte("ICO"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, body := get(t, ctx, "/favicon.ico")
	if code != http.StatusOK || body != "ICO" {
		t.Errorf("got %d %q, want 200 ICO", code, body)
	}
}

func TestFaviconDefault(t *testing.T) {
	ctx := newBrandingContext(t)
	rec := httptest.NewRecorder()
	ctx.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "/static/favicon.ico" {
		t.Errorf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// stubTypeDB serves one type by ID and name; the embedded nil interface panics on any other call.
type stubTypeDB struct {
	typedb.TypeDB
	item typedb.EveType
}

func (s stubTypeDB) GetTypeByID(typeID int64) (typedb.EveType, bool) {
	return s.item, typeID == s.item.ID
}

func (s stubTypeDB) GetType(typeName string) (typedb.EveType, bool) {
	return s.item, typeName == s.item.Name
}

// stubPriceDB reports no market data for any type.
type stubPriceDB struct {
	evepraisal.PriceDB
}

func (stubPriceDB) GetPrice(market string, typeID int64) (evepraisal.Prices, bool) {
	return evepraisal.Prices{}, false
}

func TestItemHeaderLinksEveTycoon(t *testing.T) {
	ctx := newBrandingContext(t)
	ctx.App.TypeDB = stubTypeDB{item: typedb.EveType{ID: 34, Name: "Tritanium"}}
	ctx.App.PriceDB = stubPriceDB{}
	code, body := get(t, ctx, "/item/34")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{
		`https://evetycoon.com/market/34`,
		`EVE Tycoon`,
		`https://eveinfo.com/item/34`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "evemarketer.com") {
		t.Errorf("body still links evemarketer.com")
	}
}

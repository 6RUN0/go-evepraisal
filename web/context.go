package web

import (
	"html/template"

	"golang.org/x/oauth2"

	"github.com/evepraisal/go-evepraisal"
	"github.com/gorilla/sessions"
)

// Branding defaults reproduce the upstream Evepraisal site.
const (
	DefaultSiteName  = "Evepraisal"
	DefaultSourceURL = "https://github.com/evepraisal/go-evepraisal"
)

// DefaultAboutContactsHTML is the contact list shown on the about page.
const DefaultAboutContactsHTML = template.HTML(`<li>Send an in-game mail to: <a href="https://gate.eveonline.com/Profile/Magerawr" target="_blank">Magerawr</a></li>
<li>Open an <a href="https://github.com/evepraisal/go-evepraisal/issues?state=open" target="_blank">issue on github</a></li>
<li>Talk to <a href="https://tweetfleet.slack.com/messages/@sudorandom" target="_blank">@sudorandom</a> in the <a href="https://tweetfleet.slack.com/messages/C03CGPX82" target="_blank">#devfleet channel on the tweetfleet slack</a></li>
<li>Mention <a href="https://twitter.com/sudorandom" target="_blank">@sudorandom</a> on twitter.</a></li>`)

// Context contains all of the 'global' app context for the HTTP app
type Context struct {
	App                 *evepraisal.App
	BaseURL             string
	ExtraJS             string
	ExtraHTMLHeader     string
	AdBlock             string
	CookieStore         *sessions.CookieStore
	OauthConfig         *oauth2.Config
	OauthVerifyURL      string
	ExtraStaticFilePath string
	SiteName            string
	SourceURL           string
	AboutContactsHTML   template.HTML
	FaviconPath         string

	templates map[string]*template.Template
	etags     map[string]string
}

// NewContext returns a new Context object given an app instance
func NewContext(app *evepraisal.App) *Context {
	ctx := &Context{
		App:               app,
		SiteName:          DefaultSiteName,
		SourceURL:         DefaultSourceURL,
		AboutContactsHTML: DefaultAboutContactsHTML,
	}
	ctx.GenerateStaticEtags()
	return ctx
}

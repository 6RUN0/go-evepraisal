package web

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/NYTimes/gziphandler"
	"github.com/go-zoo/bone"
	"github.com/gorilla/context"
	accesslog "github.com/mash/go-accesslog"
	newrelic "github.com/newrelic/go-agent/v3/newrelic"
)

// HandleIndex is the handler for /
func (ctx *Context) HandleIndex(w http.ResponseWriter, r *http.Request) {
	total, err := ctx.App.AppraisalDB.TotalAppraisals()
	if err != nil {
		ctx.renderServerError(r, w, err)
		return
	}
	_ = ctx.render(r, w, "main.html", struct {
		TotalAppraisalCount int64 `json:"total_appraisal_count"`
	}{TotalAppraisalCount: total})
}

// HandleNewAppraisal is the handler for /new-appraisal
func (ctx *Context) HandleNewAppraisal(w http.ResponseWriter, r *http.Request) {
	_ = ctx.render(r, w, "new-appraisal.html", struct {
		ShowLargePastePanel bool `json:"show_large_paste_panel"`
	}{ShowLargePastePanel: true})
}

// HandleLegal is the handler for /legal
func (ctx *Context) HandleLegal(w http.ResponseWriter, r *http.Request) {
	_ = ctx.render(r, w, "legal.html", nil)
}

// HandleAbout is the handler for /about
func (ctx *Context) HandleAbout(w http.ResponseWriter, r *http.Request) {
	_ = ctx.render(r, w, "about.html", nil)
}

// HandleAPIDocs is the handler for /api-docs
func (ctx *Context) HandleAPIDocs(w http.ResponseWriter, r *http.Request) {
	_ = ctx.render(r, w, "api.html", nil)
}

// HandleRobots is the handler for /robots.txt
func (ctx *Context) HandleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.WriteString(w, `User-agent: *
Disallow:`)
}

// HandleFavicon is the handler for /favicon.ico
func (ctx *Context) HandleFavicon(w http.ResponseWriter, r *http.Request) {
	if ctx.FaviconPath != "" {
		http.ServeFile(w, r, ctx.FaviconPath)
		return
	}
	http.Redirect(w, r, "static/favicon.ico", http.StatusPermanentRedirect)
}

func (ctx *Context) authWrapper(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := ctx.GetCurrentUser(r)
		if user == nil {
			ctx.renderErrorPage(r, w, http.StatusUnauthorized, "Not logged in", "You need to be logged in to see this page")
			return
		}

		handler(w, r)
	}
}

func cors(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("format") == "json" || r.Header.Get("format") == "raw" {
			w.Header().Add("Access-Control-Allow-Origin", "*")
			w.Header().Add("Access-Control-Allow-Methods", "GET")
		}
		handler(w, r)
	}
}

// HTTPHandler returns all HTTP handlers for the app
func (ctx *Context) HTTPHandler() http.Handler {

	router := bone.New()
	router.GetFunc("/", cors(ctx.HandleIndex))
	router.PostFunc("/", cors(ctx.HandleIndex))
	router.GetFunc("/new-appraisal", ctx.HandleNewAppraisal)
	router.GetFunc("/new-appraisal", ctx.HandleNewAppraisal)
	router.GetFunc("/appraisal", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusTemporaryRedirect) })

	// Create Appraisal
	router.PostFunc("/appraisal", ctx.HandleAppraisal)
	router.PostFunc("/estimate", ctx.HandleAppraisal)
	router.PostFunc("/appraisal/structured", ctx.HandleAppraisalStructured)

	// Lates Appraisals
	router.GetFunc("/latest", cors(ctx.HandleLatestAppraisals))

	// View Appraisal
	router.GetFunc("/a/#appraisalID^[a-zA-Z0-9]+$", cors(ctx.HandleViewAppraisal))
	router.GetFunc("/a/#appraisalID^[a-zA-Z0-9]+$/#privateToken^[a-zA-Z0-9]+$", cors(ctx.HandleViewAppraisal))
	router.GetFunc("/e/#legacyAppraisalID^[0-9]+$", cors(ctx.HandleViewAppraisal))

	// View Item
	router.GetFunc(`/item/#typeID^[\S ]+$`, cors(ctx.HandleViewItem))

	// List items
	router.GetFunc("/items", cors(ctx.HandleViewItems))

	// Search
	router.GetFunc("/search", cors(ctx.HandleSearch))

	// Misc
	router.GetFunc("/legal", cors(ctx.HandleLegal))
	router.GetFunc("/about", cors(ctx.HandleAbout))
	router.GetFunc("/api-docs", cors(ctx.HandleAPIDocs))
	router.GetFunc("/about/api", cors(ctx.HandleAPIDocs))
	router.GetFunc("/robots.txt", cors(ctx.HandleRobots))
	router.GetFunc("/favicon.ico", cors(ctx.HandleFavicon))

	// Authenticated pages
	router.GetFunc("/login", ctx.HandleLogin)
	router.GetFunc("/logout", ctx.HandleLogout)
	router.GetFunc("/oauthcallback", ctx.HandleAuthCallback)
	router.GetFunc("/user/history", ctx.authWrapper(ctx.HandleUserHistoryAppraisals))
	router.PostFunc("/user/history", ctx.authWrapper(ctx.HandleUserHistoryAppraisals))
	router.PostFunc("/a/delete/#appraisalID^[a-zA-Z0-9]+$", ctx.authWrapper(ctx.HandleDeleteAppraisal))

	if ctx.ExtraStaticFilePath != "" {
		files, err := extraStaticFiles(ctx.ExtraStaticFilePath)
		if err != nil {
			log.Println(err)
		}
		for httpPath, path := range files {
			log.Printf("Adding %s as %s", path, httpPath)
			body, err := os.ReadFile(path)
			if err != nil {
				log.Printf("ERROR: not able to read static file: %s", err)
				continue
			}
			contentType := mime.TypeByExtension(filepath.Ext(path))
			router.GetFunc(
				httpPath,
				func(w http.ResponseWriter, r *http.Request) {
					// An unknown extension leaves the type to net/http sniffing.
					if contentType != "" {
						w.Header().Set("Content-Type", contentType)
					}
					w.Header().Set("X-Content-Type-Options", "nosniff")
					_, _ = w.Write(body)
				})
		}
	}

	router.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx.renderErrorPage(r, w, http.StatusNotFound, "Not Found", "I couldn't find what you're looking for")
	}))

	if ctx.App.NewRelicApplication != nil {
		for _, routes := range router.Routes {
			for _, route := range routes {
				_, h := newrelic.WrapHandle(ctx.App.NewRelicApplication, route.Path, route.Handler)
				route.Handler = h
			}
		}
	}

	mux := http.NewServeMux()
	setStaticHeaders := func(h http.Handler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Cache-Control", "public, max-age=3600")
			etag, ok := ctx.etags[r.RequestURI]
			if ok {
				w.Header().Add("Etag", etag)
			}

			h.ServeHTTP(w, r)
		}
	}

	// Route our bundled static files
	mux.Handle("/static/", setStaticHeaders(http.StripPrefix("/static/", http.FileServer(http.FS(StaticFS)))))

	// Mount our web app router to root
	mux.Handle("/", router)
	err := ctx.Reload()
	if err != nil {
		log.Fatal(err)
	}

	// Wrap global handlers
	handler := http.Handler(mux)
	alogger := accessLogger{}
	userLoggerInjectHandler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := ctx.GetCurrentUser(r)
			if user != nil {
				r.Header.Set("logged-in-user", user.CharacterName)
			} else {
				r.Header.Set("logged-in-user", "")
			}

			next.ServeHTTP(w, r)
		})
	}

	formatHandler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".json") {
				r.URL.RawPath = strings.TrimSuffix(r.URL.EscapedPath(), ".json")
				r.URL.Path = strings.TrimSuffix(r.URL.Path, ".json")
				r.Header.Set("format", "json")
			} else if strings.HasSuffix(r.URL.Path, ".raw") {
				r.URL.RawPath = strings.TrimSuffix(r.URL.EscapedPath(), ".raw")
				r.URL.Path = strings.TrimSuffix(r.URL.Path, ".raw")
				r.Header.Set("format", "raw")
			} else if strings.HasSuffix(r.URL.Path, ".debug") {
				r.URL.RawPath = strings.TrimSuffix(r.URL.EscapedPath(), ".debug")
				r.URL.Path = strings.TrimSuffix(r.URL.Path, ".debug")
				r.Header.Set("format", "debug")
			} else {
				r.Header.Set("format", "")
			}
			next.ServeHTTP(w, r)
		})
	}

	handler = userLoggerInjectHandler(handler)
	handler = formatHandler(handler)
	handler = accesslog.NewLoggingHandler(handler, alogger)
	handler = context.ClearHandler(handler)
	handler = gziphandler.GzipHandler(handler)

	return handler
}

// ValidateExtraStaticFilePath fails when extra-static-file-path is set but unusable.
func (ctx *Context) ValidateExtraStaticFilePath() error {
	if ctx.ExtraStaticFilePath == "" {
		return nil
	}
	_, err := extraStaticFiles(ctx.ExtraStaticFilePath)
	return err
}

// extraStaticFiles maps the route of every regular file under root to its path.
// The root is resolved first: a trailing slash would eat the leading slash of
// every route, and WalkDir does not descend into a symlinked root. Entries
// that are not regular files are skipped without a log line: a symlink could
// serve any file the process can read, and reading a FIFO or device on start
// blocks or never ends.
func extraStaticFiles(root string) (map[string]string, error) {
	files := map[string]string{}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return files, fmt.Errorf("extra-static-file-path %q: %w", filepath.Clean(root), err)
	}
	root = resolved
	info, err := os.Stat(root)
	if err == nil && !info.IsDir() {
		err = errors.New("not a directory")
	}
	if err != nil {
		return files, fmt.Errorf("extra-static-file-path %q: %w", root, err)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files["/"+filepath.ToSlash(rel)] = path
		return nil
	})
	if err != nil {
		return files, fmt.Errorf("extra-static-file-path %q: %w", root, err)
	}
	return files, nil
}

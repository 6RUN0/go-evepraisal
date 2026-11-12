// Package management serves the internal management HTTP endpoints, such as database backups.
package management

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"expvar"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/boltdb/bolt"
	"github.com/evepraisal/go-evepraisal"
	boltdb "github.com/evepraisal/go-evepraisal/bolt"
	"github.com/husobee/vestigo"
)

// Context is the context that the web management needs
type Context struct {
	App                 *evepraisal.App
	AppraisalBackupPath string
}

// HandleRestore is the handler for /restore, this is only used for partial restores
func (ctx *Context) HandleRestore(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var appraisal evepraisal.Appraisal
	err := json.NewDecoder(r.Body).Decode(&appraisal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = ctx.App.AppraisalDB.PutNewAppraisal(&appraisal)
	if errors.Is(err, evepraisal.ErrAppraisalExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// HandleBackup is the handler for /backup/appraisals
func (ctx *Context) HandleBackup(w http.ResponseWriter, req *http.Request) {
	db, ok := ctx.App.AppraisalDB.(*boltdb.AppraisalDB)
	if !ok {
		http.Error(w, "backup not supported for this database", http.StatusInternalServerError)
		return
	}
	err := db.DB.View(func(tx *bolt.Tx) error {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="appraisals"`)
		w.Header().Set("Content-Length", strconv.Itoa(int(tx.Size())))
		_, err := tx.WriteTo(w)
		return err
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// HandleBackupToFile is the handler for /backup-to-file/appraisals
func (ctx *Context) HandleBackupToFile(w http.ResponseWriter, req *http.Request) {
	db, ok := ctx.App.AppraisalDB.(*boltdb.AppraisalDB)
	if !ok {
		http.Error(w, "backup not supported for this database", http.StatusInternalServerError)
		return
	}

	err := writeBackup(ctx.AppraisalBackupPath, func(w io.Writer) error {
		return db.DB.View(func(tx *bolt.Tx) error {
			return tx.Copy(w)
		})
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// writeBackup gzips what write produces into a temp file next to path and
// renames it over path only on success, so a failed run keeps the last good backup.
func writeBackup(path string, write func(io.Writer) error) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()

	zw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return err
	}
	if err = write(zw); err != nil {
		return err
	}
	if err = zw.Close(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// HTTPHandler returns the http.Handler for the web management api
func HTTPHandler(app *evepraisal.App, appraisalBackupPath string) http.Handler {
	ctx := Context{App: app, AppraisalBackupPath: appraisalBackupPath}
	router := vestigo.NewRouter()
	router.Get("/backup/appraisals", ctx.HandleBackup)
	router.Get("/backup-to-file/appraisals", ctx.HandleBackupToFile)
	router.Post("/restore", ctx.HandleRestore)
	router.Handle("/expvar", expvar.Handler())
	return router
}

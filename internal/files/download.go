package files

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

func (f *Feature) onFileDownload(e *core.FileDownloadRequestEvent) error {
	if f.cache == nil || !f.cache.Enabled() {
		return e.Next()
	}
	if e.App == nil || !e.App.Settings().S3.Enabled {
		return e.Next()
	}

	orig := e.Response
	err := f.cache.ServeOrTee(e.Response, e.Request, e.ServedPath, e.ServedName, func(w http.ResponseWriter) error {
		e.Response = w
		nextErr := e.Next()
		e.Response = orig
		return nextErr
	})
	e.Response = orig
	return err
}

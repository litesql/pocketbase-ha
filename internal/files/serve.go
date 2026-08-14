package files

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var inlineServeContentTypes = []string{
	"image/png", "image/jpg", "image/jpeg", "image/gif", "image/webp", "image/x-icon", "image/bmp",
	"video/webm", "video/mp4", "video/3gpp", "video/quicktime", "video/x-ms-wmv",
	"audio/basic", "audio/aiff", "audio/mpeg", "audio/midi", "audio/mp3", "audio/wave",
	"audio/wav", "audio/x-wav", "audio/x-mpeg", "audio/x-m4a", "audio/aac",
	"application/pdf", "application/x-pdf",
}

var manualExtensionContentTypes = map[string]string{
	".svg":  "image/svg+xml",
	".css":  "text/css",
	".js":   "text/javascript",
	".mjs":  "text/javascript",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

const (
	forceAttachmentParam = "download"
	defaultCacheControl  = "max-age=2592000, stale-while-revalidate=86400"
	defaultCSP           = "default-src 'none'; media-src 'self'; style-src 'unsafe-inline'; sandbox"
)

// ServeOrTee serves key from disk on hit. On miss it calls next while teeing a
// complete 200 body into the cache (no-op when the cache is disabled).
func (c *Cache) ServeOrTee(w http.ResponseWriter, r *http.Request, key, servedName string, next func(http.ResponseWriter) error) error {
	if c == nil || !c.Enabled() {
		return next(w)
	}
	if served, err := tryServeCached(c, w, r, key, servedName); served || err != nil {
		return err
	}
	var filled bool
	shared, err := c.populate(key, func() error {
		filled = true
		if served, err := tryServeCached(c, w, r, key, servedName); served || err != nil {
			return err
		}
		tmp, err := c.tmpPath()
		if err != nil {
			return next(w)
		}
		epoch := c.Epoch()
		tw := newTeeWriter(w, tmp, c.cap)
		err = next(tw)
		meta := fileMeta{
			ContentType: tw.Header().Get("Content-Type"),
			Name:        servedName,
			ModTime:     time.Now(),
		}
		if err != nil || !tw.commitOK(r) {
			_ = os.Remove(tmp)
			return err
		}
		if putErr := c.commit(key, tmp, meta, epoch); putErr != nil {
			_ = os.Remove(tmp)
		}
		return nil
	})
	if shared && !filled {
		if served, serveErr := tryServeCached(c, w, r, key, servedName); served || serveErr != nil {
			return serveErr
		}
		return next(w)
	}
	return err
}

func tryServeCached(c *Cache, w http.ResponseWriter, r *http.Request, key, servedName string) (bool, error) {
	if c == nil || !c.Enabled() {
		return false, nil
	}
	path, meta, ok := c.Get(key)
	if !ok {
		return false, nil
	}
	err := serveCached(w, r, path, servedName, meta)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func serveCached(w http.ResponseWriter, r *http.Request, abs, name string, meta fileMeta) error {
	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer f.Close()

	var forceAttachment bool
	if raw := r.URL.Query().Get(forceAttachmentParam); raw != "" {
		forceAttachment, _ = strconv.ParseBool(raw)
	}

	ct := meta.ContentType
	if ct == "" {
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		ct = http.DetectContentType(buf[:n])
		_, _ = f.Seek(0, io.SeekStart)
	}
	if extCT, found := manualExtensionContentTypes[filepath.Ext(abs)]; found {
		ct = extCT
	}

	disposition := "attachment"
	if !forceAttachment && contains(inlineServeContentTypes, ct) {
		disposition = "inline"
	}

	setHeaderIfMissing(w, "Content-Disposition", disposition+"; filename="+name)
	setHeaderIfMissing(w, "Content-Type", ct)
	setHeaderIfMissing(w, "Content-Security-Policy", defaultCSP)
	setHeaderIfMissing(w, "Cache-Control", defaultCacheControl)

	mod := meta.ModTime
	if mod.IsZero() {
		if info, err := f.Stat(); err == nil {
			mod = info.ModTime()
		}
	}
	http.ServeContent(w, r, name, mod, f)
	return nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func setHeaderIfMissing(res http.ResponseWriter, key, value string) {
	if _, ok := res.Header()[key]; !ok {
		res.Header().Set(key, value)
	}
}

type teeWriter struct {
	http.ResponseWriter
	status  int
	tmp     *os.File
	tmpPath string
	written int64
	cap     int64
	failed  bool
}

func newTeeWriter(w http.ResponseWriter, tmpPath string, capBytes int64) *teeWriter {
	f, err := os.Create(tmpPath)
	tw := &teeWriter{ResponseWriter: w, tmp: f, tmpPath: tmpPath, cap: capBytes}
	if err != nil {
		tw.failed = true
	}
	return tw
}

func (t *teeWriter) WriteHeader(code int) {
	if t.status == 0 {
		t.status = code
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *teeWriter) Write(p []byte) (int, error) {
	if t.status == 0 {
		t.WriteHeader(http.StatusOK)
	}
	n, err := t.ResponseWriter.Write(p)
	if t.status == http.StatusOK && t.tmp != nil && !t.failed && n > 0 {
		if t.cap > 0 && t.written+int64(n) > t.cap {
			t.abandonTmp()
		} else if _, werr := t.tmp.Write(p[:n]); werr != nil {
			t.abandonTmp()
		} else {
			t.written += int64(n)
		}
	}
	return n, err
}

func (t *teeWriter) abandonTmp() {
	t.failed = true
	if t.tmp != nil {
		_ = t.tmp.Close()
		t.tmp = nil
	}
	if t.tmpPath != "" {
		_ = os.Remove(t.tmpPath)
	}
}

func (t *teeWriter) Unwrap() http.ResponseWriter {
	return t.ResponseWriter
}

func (t *teeWriter) Flush() {
	if f, ok := t.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (t *teeWriter) commitOK(r *http.Request) bool {
	if t.tmp != nil {
		_ = t.tmp.Close()
		t.tmp = nil
	}
	if t.failed || t.status != http.StatusOK {
		return false
	}
	if r != nil {
		if r.Method == http.MethodHead {
			return false
		}
		if r.Header.Get("Range") != "" {
			return false
		}
	}
	if t.Header().Get("Content-Encoding") != "" {
		return false
	}
	cl := t.Header().Get("Content-Length")
	if cl == "" {
		return false
	}
	n, err := strconv.ParseInt(cl, 10, 64)
	if err != nil || n != t.written {
		return false
	}
	return t.written > 0
}

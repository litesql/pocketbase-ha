package files

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServeOrTeeMissWritesCache(t *testing.T) {
	c := testCache(t, 1024)
	var nextCalls int
	next := func(w http.ResponseWriter) error {
		nextCalls++
		body := "payload"
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
		return nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/files/col/rec/a.txt", nil)
	if err := c.ServeOrTee(rec, req, "col/rec/a.txt", "a.txt", next); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "payload" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if nextCalls != 1 {
		t.Fatalf("next calls = %d", nextCalls)
	}

	rec = httptest.NewRecorder()
	if err := c.ServeOrTee(rec, req, "col/rec/a.txt", "a.txt", next); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "payload" {
		t.Fatalf("cached body = %q", rec.Body.String())
	}
	if nextCalls != 1 {
		t.Fatal("second request must not call next")
	}
}

func TestServeOrTeeDownloadDisposition(t *testing.T) {
	c := testCache(t, 1024)
	tmp := writeTemp(t, c, "png-bytes")
	if err := c.Put("col/rec/a.png", tmp, fileMeta{ContentType: "image/png", Name: "a.png"}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/files/col/rec/a.png?download=1", nil)
	if err := c.ServeOrTee(rec, req, "col/rec/a.png", "a.png", func(http.ResponseWriter) error {
		t.Fatal("next should not run on hit")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := rec.Header().Get("Content-Disposition")
	if got != "attachment; filename=a.png" {
		t.Fatalf("Content-Disposition = %q", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/files/col/rec/a.png", nil)
	if err := c.ServeOrTee(rec, req, "col/rec/a.png", "a.png", func(http.ResponseWriter) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got = rec.Header().Get("Content-Disposition")
	if got != "inline; filename=a.png" {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestServeOrTeeDoesNotCacheErrors(t *testing.T) {
	c := testCache(t, 1024)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/files/col/rec/a.txt", nil)
	err := c.ServeOrTee(rec, req, "col/rec/a.txt", "a.txt", func(w http.ResponseWriter) error {
		http.Error(w, "nope", http.StatusNotFound)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Get("col/rec/a.txt"); ok {
		t.Fatal("404 must not be cached")
	}
}

func TestServeOrTeeDisabledCallsNext(t *testing.T) {
	c := testCache(t, 0)
	var called bool
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_ = c.ServeOrTee(rec, req, "k", "n", func(w http.ResponseWriter) error {
		called = true
		_, _ = io.WriteString(w, "x")
		return nil
	})
	if !called {
		t.Fatal("disabled cache must call next")
	}
	if _, err := os.Stat(c.Root() + "/k"); err == nil {
		t.Fatal("disabled cache must not write objects")
	}
}

func TestServeOrTeeDoesNotCacheWithoutContentLength(t *testing.T) {
	c := testCache(t, 1024)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := c.ServeOrTee(rec, req, "k", "n", func(w http.ResponseWriter) error {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "payload")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "payload" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if _, _, ok := c.Get("k"); ok {
		t.Fatal("chunked/unknown-length bodies must not be cached")
	}
}

func TestServeOrTeeDoesNotCacheOnNextError(t *testing.T) {
	c := testCache(t, 1024)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	body := "payload"
	err := c.ServeOrTee(rec, req, "k", "n", func(w http.ResponseWriter) error {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
		return io.ErrUnexpectedEOF
	})
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("err = %v", err)
	}
	if _, _, ok := c.Get("k"); ok {
		t.Fatal("failed next() must not be cached")
	}
}

func TestServeOrTeeDoesNotCacheOverCap(t *testing.T) {
	c := testCache(t, 10)
	body := strings.Repeat("a", 50)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := c.ServeOrTee(rec, req, "k", "n", func(w http.ResponseWriter) error {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != body {
		t.Fatal("client must still receive the full body")
	}
	if _, _, ok := c.Get("k"); ok {
		t.Fatal("objects larger than the cap must not be cached")
	}
}

func TestServeOrTeeCoalescesConcurrentMisses(t *testing.T) {
	c := testCache(t, 1<<20)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	body := "payload"
	var startOnce sync.Once
	next := func(w http.ResponseWriter) error {
		calls.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
		return nil
	}

	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		recs[i] = httptest.NewRecorder()
		go func(rec *httptest.ResponseRecorder) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			_ = c.ServeOrTee(rec, req, "k", "n", next)
		}(recs[i])
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("origin never started")
	}
	close(release)
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("origin calls = %d, want 1", calls.Load())
	}
	for i, rec := range recs {
		if rec.Body.String() != body {
			t.Fatalf("body[%d] = %q", i, rec.Body.String())
		}
	}
}

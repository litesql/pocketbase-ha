package files

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestShouldProxyLocal(t *testing.T) {
	if shouldProxyLocal(true, false, http.MethodGet, "/api/files/c/r/f") {
		t.Fatal("S3 must skip local proxy")
	}
	if shouldProxyLocal(false, true, http.MethodGet, "/api/files/c/r/f") {
		t.Fatal("leader must skip local proxy")
	}
	if shouldProxyLocal(false, false, http.MethodPost, "/api/files/token") {
		t.Fatal("POST must skip")
	}
	if !shouldProxyLocal(false, false, http.MethodGet, "/api/files/c/r/f") {
		t.Fatal("replica local GET files must proxy")
	}
}

func TestProxyAndMaybeCacheMissHitsLeaderThenCaches(t *testing.T) {
	var hits int
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/api/files/col/rec/a.txt" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("X-Forwarded-For") != "10.0.0.1" {
			t.Fatalf("X-Forwarded-For = %q", r.Header.Get("X-Forwarded-For"))
		}
		body := "from-leader"
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(leader.Close)

	c := testCache(t, 1024)
	req := httptest.NewRequest(http.MethodGet, "/api/files/col/rec/a.txt", nil)
	rec := httptest.NewRecorder()
	if err := proxyAndMaybeCache(c, leader.URL, rec, req, "col/rec/a.txt", "a.txt", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "from-leader" {
		t.Fatalf("body = %q", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/files/col/rec/a.txt", nil)
	rec = httptest.NewRecorder()
	if err := proxyAndMaybeCache(c, leader.URL, rec, req, "col/rec/a.txt", "a.txt", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "from-leader" {
		t.Fatalf("cached body = %q", rec.Body.String())
	}
	if hits != 1 {
		t.Fatalf("leader hits = %d, want 1", hits)
	}
}

func TestProxyAndMaybeCacheDisabledAlwaysHitsLeader(t *testing.T) {
	var hits int
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = io.WriteString(w, "x")
	}))
	t.Cleanup(leader.Close)

	c := testCache(t, 0)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/files/col/rec/a.txt", nil)
		rec := httptest.NewRecorder()
		if err := proxyAndMaybeCache(c, leader.URL, rec, req, "col/rec/a.txt", "a.txt", "10.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 2 {
		t.Fatalf("disabled cache should proxy every time, hits=%d", hits)
	}
}

func TestParseLeaderTargetRejectsNonHTTP(t *testing.T) {
	if _, err := parseLeaderTarget("ftp://leader:8090"); err == nil {
		t.Fatal("expected reject ftp")
	}
	if _, err := parseLeaderTarget("http://"); err == nil {
		t.Fatal("expected reject empty host")
	}
	if _, err := parseLeaderTarget("http://leader:8090"); err != nil {
		t.Fatal(err)
	}
}

func TestProxyAndMaybeCacheRejectsBadTarget(t *testing.T) {
	c := testCache(t, 1024)
	req := httptest.NewRequest(http.MethodGet, "/api/files/col/rec/a.txt", nil)
	rec := httptest.NewRecorder()
	if err := proxyAndMaybeCache(c, "ftp://leader:8090", rec, req, "col/rec/a.txt", "a.txt", ""); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d", rec.Code)
	}
}

package files

import (
	"net/http"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func TestFileRateLimiterAllow(t *testing.T) {
	var l fileRateLimiter
	if !l.allow("k", 2, time.Minute) || !l.allow("k", 2, time.Minute) {
		t.Fatal("first two must be allowed")
	}
	if l.allow("k", 2, time.Minute) {
		t.Fatal("third must be denied")
	}
	if !l.allow("other", 2, time.Minute) {
		t.Fatal("different key must be independent")
	}
}

func TestFileRateLimiterDropsIdleKeys(t *testing.T) {
	var l fileRateLimiter
	if !l.allow("k", 1, time.Minute) {
		t.Fatal("first must be allowed")
	}
	if l.allow("k", 1, time.Minute) {
		t.Fatal("second must be denied")
	}
	l.clean(time.Now().Add(2 * time.Minute))
	if !l.allow("k", 1, time.Minute) {
		t.Fatal("idle key must expire so the next request is allowed")
	}
}

func TestFileRateLimiterKeepsActiveKeys(t *testing.T) {
	var l fileRateLimiter
	if !l.allow("k", 2, time.Minute) {
		t.Fatal("first must be allowed")
	}
	l.clean(time.Now())
	if !l.allow("k", 2, time.Minute) {
		t.Fatal("in-window hit must survive clean")
	}
	if l.allow("k", 2, time.Minute) {
		t.Fatal("third must still be denied")
	}
}

func TestCheckCollectionFileRateLimit(t *testing.T) {
	app := testApp(t)
	col, err := app.FindCachedCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}
	app.Settings().RateLimits.Enabled = true
	app.Settings().RateLimits.Rules = []core.RateLimitRule{{
		Label:       "demo1:file",
		MaxRequests: 1,
		Duration:    60,
	}}

	f := &Feature{}
	e := fileEvent(app, protectedImageURL, nil)
	if err := f.checkCollectionFileRateLimit(e, col); err != nil {
		t.Fatalf("first request: %v", err)
	}
	err = f.checkCollectionFileRateLimit(e, col)
	if !isStatus(err, http.StatusTooManyRequests) {
		t.Fatalf("second request err = %v", err)
	}
}

func TestCheckCollectionFileRateLimitDisabled(t *testing.T) {
	app := testApp(t)
	col, err := app.FindCachedCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}
	f := &Feature{}
	e := fileEvent(app, protectedImageURL, nil)
	for i := 0; i < 3; i++ {
		if err := f.checkCollectionFileRateLimit(e, col); err != nil {
			t.Fatalf("disabled limiter err = %v", err)
		}
	}
}

package cluster

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKeepRealtimeLocalBypassesRealtimePost(t *testing.T) {
	var forwarded bool
	var handledLocal bool

	middleware := keepRealtimeLocal(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			forwarded = true
			w.WriteHeader(http.StatusTeapot)
		})
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handledLocal = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/realtime", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if forwarded {
		t.Fatal("expected realtime POST to stay local, but forward middleware was called")
	}
	if !handledLocal {
		t.Fatal("expected realtime POST to reach the local handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected local handler status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestKeepRealtimeLocalForwardsOtherPosts(t *testing.T) {
	var forwarded bool
	var handledLocal bool

	middleware := keepRealtimeLocal(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			forwarded = true
			w.WriteHeader(http.StatusAccepted)
		})
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handledLocal = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/collections/posts/records", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !forwarded {
		t.Fatal("expected non-realtime POST to use forward middleware")
	}
	if handledLocal {
		t.Fatal("expected non-realtime POST to be handled by forward middleware")
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected forward middleware status %d, got %d", http.StatusAccepted, rec.Code)
	}
}

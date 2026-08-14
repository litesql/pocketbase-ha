package files

import (
	"net/url"
	"testing"
)

func TestCacheKeyOriginalAndThumb(t *testing.T) {
	got := CacheKey("col", "rec", "photo.png", "")
	want := "col/rec/photo.png"
	if got != want {
		t.Fatalf("CacheKey original = %q, want %q", got, want)
	}

	got = CacheKey("col", "rec", "photo.png", "100x100")
	want = "col/rec/thumbs_photo.png/100x100_photo.png"
	if got != want {
		t.Fatalf("CacheKey thumb = %q, want %q", got, want)
	}
}

func TestParseFilesURLStripsTokenAndKeepsThumb(t *testing.T) {
	u, err := url.Parse("/api/files/posts/abc/file_x.png?token=secret&thumb=100x100&download=1")
	if err != nil {
		t.Fatal(err)
	}
	collection, recordID, filename, thumb, ok := ParseFilesURL(u)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if collection != "posts" || recordID != "abc" || filename != "file_x.png" {
		t.Fatalf("got %s %s %s", collection, recordID, filename)
	}
	if thumb != "100x100" {
		t.Fatalf("thumb = %q", thumb)
	}
}

func TestParseFilesURLRejectsBadPaths(t *testing.T) {
	tests := []string{
		"/api/realtime",
		"/api/files/only-two/parts",
		"/api/files/a/b/c/d",
		"/api/files/a/../b/c",
	}
	for _, raw := range tests {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, _, ok := ParseFilesURL(u); ok {
			t.Fatalf("expected reject %q", raw)
		}
	}
}

func TestShouldUseThumbPath(t *testing.T) {
	if !shouldUseThumbPath("100x100", nil, "photo.png") {
		t.Fatal("default thumb size should be allowed for images")
	}
	if !shouldUseThumbPath("200x200", []string{"200x200"}, "photo.jpg") {
		t.Fatal("configured thumb should be allowed")
	}
	if shouldUseThumbPath("999x999", []string{"200x200"}, "photo.png") {
		t.Fatal("unknown thumb should not rewrite path")
	}
	if shouldUseThumbPath("", nil, "photo.png") {
		t.Fatal("empty thumb is original")
	}
	if shouldUseThumbPath("100x100", nil, "notes.pdf") {
		t.Fatal("non-image files must not use the thumb cache key")
	}
}

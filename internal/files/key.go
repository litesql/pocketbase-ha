package files

import (
	"net/url"
	"path"
	"strings"
)

const filesPathPrefix = "/api/files/"

const defaultThumbSize = "100x100"

// CacheKey is the on-disk relative path for a PocketBase storage object.
// thumb is empty for the original file.
func CacheKey(collectionID, recordID, filename, thumb string) string {
	base := path.Join(collectionID, recordID)
	if thumb == "" {
		return path.Join(base, filename)
	}
	return path.Join(base, "thumbs_"+filename, thumb+"_"+filename)
}

// ParseFilesURL extracts collection/record/filename/thumb from a GET /api/files URL.
// token and download are ignored so they never become cache keys.
func ParseFilesURL(u *url.URL) (collection, recordID, filename, thumb string, ok bool) {
	if u == nil {
		return "", "", "", "", false
	}
	p := strings.TrimRight(u.Path, "/")
	if !strings.HasPrefix(p, filesPathPrefix) {
		return "", "", "", "", false
	}
	rest := strings.TrimPrefix(p, filesPathPrefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return "", "", "", "", false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.Contains(part, "..") {
			return "", "", "", "", false
		}
	}
	return parts[0], parts[1], parts[2], u.Query().Get("thumb"), true
}

func shouldUseThumbPath(thumb string, fieldThumbs []string, filename string) bool {
	if thumb == "" || !isImageFilename(filename) {
		return false
	}
	if thumb == defaultThumbSize {
		return true
	}
	for _, allowed := range fieldThumbs {
		if allowed == thumb {
			return true
		}
	}
	return false
}

func isImageFilename(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".heic", ".avif", ".ico":
		return true
	default:
		return false
	}
}

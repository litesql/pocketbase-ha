package files

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

var errInvalidLeaderTarget = errors.New("invalid leader target")

const (
	leaderDialTimeout   = 10 * time.Second
	leaderHeaderTimeout = 30 * time.Second
	leaderTLSTimeout    = 10 * time.Second
	leaderIdleTimeout   = 90 * time.Second
)

var leaderTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{
		Timeout:   leaderDialTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	t.ResponseHeaderTimeout = leaderHeaderTimeout
	t.TLSHandshakeTimeout = leaderTLSTimeout
	t.IdleConnTimeout = leaderIdleTimeout
	return t
}()

func shouldProxyLocal(s3Enabled, isLeader bool, method, path string) bool {
	if s3Enabled || isLeader {
		return false
	}
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	return strings.HasPrefix(path, filesPathPrefix)
}

func (f *Feature) replicaFileProxy(e *core.RequestEvent) error {
	if e.App != nil && e.App.Settings().S3.Enabled {
		return e.Next()
	}
	if !isFilesDownload(e.Request) {
		return e.Next()
	}

	leader := f.leader()
	if leader != nil && leader.IsLeader() {
		return e.Next()
	}

	resolved, err := authorizeFileRequest(e)
	if err != nil {
		return err
	}

	if err := f.checkCollectionFileRateLimit(e, resolved.collection); err != nil {
		return err
	}

	e.Response.Header().Del("X-Frame-Options")

	if served, serveErr := tryServeCached(f.cache, e.Response, e.Request, resolved.servedPath, resolved.servedName); served || serveErr != nil {
		return serveErr
	}

	target := ""
	if leader != nil {
		target = strings.TrimRight(leader.RedirectTarget(), "/")
	}
	if target == "" {
		slog.Warn("local file proxy: no leader redirect target")
		http.Error(e.Response, "leader file fetch failed", http.StatusBadGateway)
		return nil
	}

	return proxyAndMaybeCache(f.cache, target, e.Response, e.Request, resolved.servedPath, resolved.servedName, e.RealIP())
}

func parseLeaderTarget(target string) (*url.URL, error) {
	dest, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	if dest.Scheme != "http" && dest.Scheme != "https" {
		return nil, errInvalidLeaderTarget
	}
	if dest.Host == "" {
		return nil, errInvalidLeaderTarget
	}
	return dest, nil
}

func newLeaderProxy(dest *url.URL, clientIP string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		FlushInterval: 100 * time.Millisecond,
		Transport:     leaderTransport,
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(rw, "leader file fetch failed", http.StatusBadGateway)
		},
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(dest)
			if clientIP != "" {
				pr.Out.Header.Set("X-Forwarded-For", clientIP)
			} else {
				pr.SetXForwarded()
			}
		},
	}
}

func proxyAndMaybeCache(cache *Cache, target string, w http.ResponseWriter, r *http.Request, key, servedName, clientIP string) error {
	dest, err := parseLeaderTarget(target)
	if err != nil {
		http.Error(w, "invalid leader target", http.StatusBadGateway)
		return nil
	}

	proxy := newLeaderProxy(dest, clientIP)

	if cache == nil || !cache.Enabled() || r.Method == http.MethodHead {
		proxy.ServeHTTP(w, r)
		return nil
	}

	if served, serveErr := tryServeCached(cache, w, r, key, servedName); served || serveErr != nil {
		return serveErr
	}

	var filled bool
	shared, err := cache.populate(key, func() error {
		filled = true
		if served, serveErr := tryServeCached(cache, w, r, key, servedName); served || serveErr != nil {
			return serveErr
		}
		tmp, err := cache.tmpPath()
		if err != nil {
			proxy.ServeHTTP(w, r)
			return nil
		}
		epoch := cache.Epoch()
		tw := newTeeWriter(w, tmp, cache.cap)
		proxy.ServeHTTP(tw, r)
		meta := fileMeta{
			ContentType: tw.Header().Get("Content-Type"),
			Name:        servedName,
			ModTime:     time.Now(),
		}
		if !tw.commitOK(r) {
			_ = os.Remove(tmp)
			return nil
		}
		_ = cache.commit(key, tmp, meta, epoch)
		return nil
	})
	if shared && !filled {
		if served, serveErr := tryServeCached(cache, w, r, key, servedName); served || serveErr != nil {
			return serveErr
		}
		proxy.ServeHTTP(w, r)
		return nil
	}
	return err
}

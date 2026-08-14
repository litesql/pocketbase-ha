package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/litesql/go-ha"
	"github.com/litesql/pocketbase-ha/internal/config"
	"github.com/litesql/pocketbase-ha/internal/feature"
	"github.com/pocketbase/pocketbase/core"
)

const localCacheDirName = "filecache"

var _ feature.Feature = (*Feature)(nil)

// LeaderSource is the cluster Feature (or a test fake) that owns the
// data.db LeaderProvider. Files does not scan ha.ListDSN().
type LeaderSource interface {
	LeaderProvider() ha.LeaderProvider
}

// Feature serves replica local files via the leader and optionally hot-caches
// S3/local file responses on disk.
type Feature struct {
	cfg     config.Config
	cache   *Cache
	leaders LeaderSource
	limiter fileRateLimiter

	mu      sync.Mutex
	lastS3  bool
	s3Known bool
}

func New(cfg config.Config, leaders LeaderSource) *Feature {
	return &Feature{cfg: cfg, leaders: leaders}
}

func (f *Feature) Name() string {
	return "files"
}

func (f *Feature) Register(app core.App) error {
	dir, err := resolveCacheDir(app.DataDir(), f.cfg.FileCacheDir)
	if err != nil {
		return err
	}

	cache, err := NewCache(dir, f.cfg.FileCacheSizeBytes)
	if err != nil {
		return err
	}
	f.cache = cache

	app.OnFileDownloadRequest().BindFunc(f.onFileDownload)
	app.OnModelAfterUpdateSuccess().BindFunc(f.onModelAfterChange)
	app.OnModelAfterDeleteSuccess().BindFunc(f.onModelAfterChange)
	app.OnSettingsReload().BindFunc(f.onSettingsReload)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		f.snapshotS3(se.App)
		se.Router.BindFunc(f.replicaFileProxy)
		return se.Next()
	})
	_ = app.Cron().Add("pbhaFileRateLimitCleanup", "2 * * * *", func() {
		f.limiter.clean(time.Now())
	})

	return nil
}

func (f *Feature) leader() ha.LeaderProvider {
	if f == nil || f.leaders == nil {
		return nil
	}
	return f.leaders.LeaderProvider()
}

func resolveCacheDir(dataDir, configured string) (string, error) {
	dir := strings.TrimSpace(configured)
	if dir == "" {
		dir = filepath.Join(dataDir, localCacheDirName)
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(dataDir, dir)
	}
	dir = filepath.Clean(dir)
	if err := checkCacheDir(dataDir, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func checkCacheDir(dataDir, cacheDir string) error {
	dataAbs, err := absEval(dataDir)
	if err != nil {
		return err
	}
	cacheAbs, err := absEval(cacheDir)
	if err != nil {
		return err
	}
	storageAbs := filepath.Join(dataAbs, "storage")
	if cacheAbs == dataAbs {
		return fmt.Errorf("PB_FILECACHE_DIR cannot be the data directory %q", dataAbs)
	}
	if cacheAbs == storageAbs || isInside(cacheAbs, storageAbs) {
		return fmt.Errorf("PB_FILECACHE_DIR cannot sit inside storage/ (%q)", storageAbs)
	}
	if isInside(storageAbs, cacheAbs) {
		return fmt.Errorf("PB_FILECACHE_DIR cannot contain storage/ (%q)", cacheAbs)
	}
	return nil
}

func absEval(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	dir := abs
	var parts []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs, nil
		}
		parts = append([]string{filepath.Base(dir)}, parts...)
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(append([]string{resolved}, parts...)...), nil
		}
		dir = parent
	}
}

func isInside(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	sep := string(os.PathSeparator)
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+sep)
}

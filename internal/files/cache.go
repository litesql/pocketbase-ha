package files

import (
	"container/heap"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	metaSuffix = ".meta"
	tmpDirName = ".tmp"
)

var errBadKey = errors.New("invalid cache key")

type fileMeta struct {
	ContentType string    `json:"contentType"`
	Name        string    `json:"name"`
	ModTime     time.Time `json:"modTime"`
}

type cacheEntry struct {
	rel   string
	size  int64
	atime time.Time
	index int
}

type atimeHeap []*cacheEntry

func (h atimeHeap) Len() int { return len(h) }

func (h atimeHeap) Less(i, j int) bool { return h[i].atime.Before(h[j].atime) }

func (h atimeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *atimeHeap) Push(x any) {
	e := x.(*cacheEntry)
	e.index = len(*h)
	*h = append(*h, e)
}

func (h *atimeHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	e.index = -1
	*h = old[:n-1]
	return e
}

// Cache is a byte-capped disk LRU of PocketBase file objects.
type Cache struct {
	root string
	cap  int64

	mu    sync.Mutex
	used  int64
	idx   map[string]*cacheEntry
	h     atimeHeap
	epoch uint64

	sf singleflight.Group
}

// NewCache prepares root and indexes existing files. cap 0 disables put/get
// and does not create the directory.
func NewCache(root string, capBytes int64) (*Cache, error) {
	if capBytes < 0 {
		capBytes = 0
	}
	c := &Cache{
		root: root,
		cap:  capBytes,
		idx:  map[string]*cacheEntry{},
	}
	if capBytes == 0 {
		return c, nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(filepath.Join(root, tmpDirName))
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, tmpDirName+"/") || strings.HasSuffix(rel, metaSuffix) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size := info.Size()
		if metaInfo, err := os.Stat(p + metaSuffix); err == nil {
			size += metaInfo.Size()
		}
		e := &cacheEntry{rel: rel, size: size, atime: info.ModTime()}
		c.idx[rel] = e
		c.used += size
		c.h = append(c.h, e)
		return nil
	})
	for i, e := range c.h {
		e.index = i
	}
	heap.Init(&c.h)
	c.evictLocked()
	return c, nil
}

// Enabled reports whether new objects should be stored and served from disk.
func (c *Cache) Enabled() bool {
	return c != nil && c.cap > 0
}

// Root returns the cache directory.
func (c *Cache) Root() string {
	if c == nil {
		return ""
	}
	return c.root
}

// Epoch is bumped on Flush and DeletePrefix so in-flight tees cannot
// re-commit bytes that were invalidated while they were downloading.
func (c *Cache) Epoch() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.epoch
}

func (c *Cache) populate(key string, fn func() error) (shared bool, err error) {
	_, err, shared = c.sf.Do(key, func() (any, error) {
		return nil, fn()
	})
	return shared, err
}

func (c *Cache) abs(rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "/")
	clean := pathClean(rel)
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", errBadKey
	}
	full := filepath.Join(c.root, filepath.FromSlash(clean))
	relToRoot, err := filepath.Rel(c.root, full)
	if err != nil || strings.HasPrefix(relToRoot, "..") {
		return "", errBadKey
	}
	return full, nil
}

func pathClean(rel string) string {
	parts := strings.Split(rel, "/")
	var out []string
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			if len(out) == 0 {
				return ".."
			}
			out = out[:len(out)-1]
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "/")
}

// Get returns the absolute path of a cached object.
func (c *Cache) Get(rel string) (string, fileMeta, bool) {
	if !c.Enabled() {
		return "", fileMeta{}, false
	}
	full, err := c.abs(rel)
	if err != nil {
		return "", fileMeta{}, false
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return "", fileMeta{}, false
	}
	meta := fileMeta{ModTime: info.ModTime()}
	if raw, err := os.ReadFile(full + metaSuffix); err == nil {
		_ = json.Unmarshal(raw, &meta)
		if meta.ModTime.IsZero() {
			meta.ModTime = info.ModTime()
		}
	}
	c.mu.Lock()
	if e, ok := c.idx[rel]; ok {
		e.atime = time.Now()
		heap.Fix(&c.h, e.index)
	}
	c.mu.Unlock()
	return full, meta, true
}

// Put commits tmp into the cache under rel and evicts oldest entries if needed.
func (c *Cache) Put(rel, tmp string, meta fileMeta) error {
	return c.commit(rel, tmp, meta, c.Epoch())
}

func (c *Cache) commit(rel, tmp string, meta fileMeta, epoch uint64) error {
	if !c.Enabled() {
		_ = os.Remove(tmp)
		return nil
	}
	full, err := c.abs(rel)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	info, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	if c.cap > 0 && info.Size() > c.cap {
		_ = os.Remove(tmp)
		return nil
	}

	c.mu.Lock()
	if epoch != c.epoch {
		c.mu.Unlock()
		_ = os.Remove(tmp)
		return nil
	}
	c.mu.Unlock()

	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(full, 0o600)
	if meta.ModTime.IsZero() {
		meta.ModTime = info.ModTime()
	}
	raw, _ := json.Marshal(meta)
	metaPath := full + metaSuffix
	_ = os.WriteFile(metaPath, raw, 0o600)
	size := info.Size()
	if mi, err := os.Stat(metaPath); err == nil {
		size += mi.Size()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if epoch != c.epoch {
		_ = os.Remove(full)
		_ = os.Remove(metaPath)
		return nil
	}
	if old, ok := c.idx[rel]; ok {
		c.used -= old.size
		c.removeFromHeap(old)
	}
	e := &cacheEntry{rel: rel, size: size, atime: time.Now()}
	c.idx[rel] = e
	heap.Push(&c.h, e)
	c.used += size
	c.evictLocked()
	return nil
}

func (c *Cache) removeFromHeap(e *cacheEntry) {
	if e.index < 0 || e.index >= c.h.Len() {
		return
	}
	heap.Remove(&c.h, e.index)
}

func (c *Cache) evictLocked() {
	for c.used > c.cap && c.h.Len() > 0 {
		v := heap.Pop(&c.h).(*cacheEntry)
		delete(c.idx, v.rel)
		full, err := c.abs(v.rel)
		if err != nil {
			c.used -= v.size
			if c.used < 0 {
				c.used = 0
			}
			continue
		}
		_ = os.Remove(full)
		_ = os.Remove(full + metaSuffix)
		c.used -= v.size
		if c.used < 0 {
			c.used = 0
		}
	}
}

// DeletePrefix removes every cached object under prefix (record or collection dir).
func (c *Cache) DeletePrefix(prefix string) {
	if c == nil {
		return
	}
	prefix = strings.Trim(filepath.ToSlash(prefix), "/")
	if prefix == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	var toDelete []string
	for rel := range c.idx {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			toDelete = append(toDelete, rel)
		}
	}
	for _, rel := range toDelete {
		e := c.idx[rel]
		delete(c.idx, rel)
		if e != nil {
			c.removeFromHeap(e)
			c.used -= e.size
		}
		full, err := c.abs(rel)
		if err != nil {
			continue
		}
		_ = os.Remove(full)
		_ = os.Remove(full + metaSuffix)
	}
	if c.used < 0 {
		c.used = 0
	}
	dir, err := c.abs(prefix)
	if err == nil {
		_ = os.RemoveAll(dir)
	}
}

// Flush deletes every cached object.
func (c *Cache) Flush() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	entries, _ := os.ReadDir(c.root)
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(c.root, e.Name()))
	}
	c.idx = map[string]*cacheEntry{}
	c.h = nil
	c.used = 0
	heap.Init(&c.h)
	if c.cap == 0 {
		return nil
	}
	return os.MkdirAll(c.root, 0o700)
}

func (c *Cache) tmpPath() (string, error) {
	dir := filepath.Join(c.root, tmpDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "*.part")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Chmod(name, 0o600)
	return name, nil
}

// Used returns current byte usage. For tests.
func (c *Cache) Used() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used
}

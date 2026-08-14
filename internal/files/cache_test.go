package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCachePutGetAndPrefixDelete(t *testing.T) {
	c := testCache(t, 10<<20)

	key := CacheKey("col", "rec", "a.txt", "")
	tmp := writeTemp(t, c, "hello")
	if err := c.Put(key, tmp, fileMeta{ContentType: "text/plain", Name: "a.txt"}); err != nil {
		t.Fatal(err)
	}

	path, meta, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q", body)
	}
	if meta.ContentType != "text/plain" {
		t.Fatalf("content-type = %q", meta.ContentType)
	}

	thumb := CacheKey("col", "rec", "a.txt", "100x100")
	tmp = writeTemp(t, c, "thumb")
	if err := c.Put(thumb, tmp, fileMeta{Name: "100x100_a.txt"}); err != nil {
		t.Fatal(err)
	}

	c.DeletePrefix("col/rec")
	if _, _, ok := c.Get(key); ok {
		t.Fatal("original should be gone")
	}
	if _, _, ok := c.Get(thumb); ok {
		t.Fatal("thumb should be gone")
	}
}

func TestCacheEvictsOldestUnderCap(t *testing.T) {
	c := testCache(t, 250)

	if err := c.Put("a.txt", writeTemp(t, c, string(make([]byte, 100))), fileMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("b.txt", writeTemp(t, c, string(make([]byte, 100))), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	if _, _, ok := c.Get("a.txt"); ok {
		t.Fatal("expected a.txt to be evicted")
	}
	if _, _, ok := c.Get("b.txt"); !ok {
		t.Fatal("expected b.txt to remain")
	}
}

func TestCacheDisabledSkipsGetAndPut(t *testing.T) {
	c := testCache(t, 0)
	if c.Enabled() {
		t.Fatal("cap 0 must be disabled")
	}
	tmp := writeTemp(t, c, "x")
	if err := c.Put("a.txt", tmp, fileMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Get("a.txt"); ok {
		t.Fatal("disabled cache must not hit")
	}
}

func TestCacheFlush(t *testing.T) {
	c := testCache(t, 1024)
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Get("a.txt"); ok {
		t.Fatal("flush should empty the cache")
	}
}

func testCache(t *testing.T, cap int64) *Cache {
	t.Helper()
	c, err := NewCache(t.TempDir(), cap)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeTemp(t *testing.T, c *Cache, body string) string {
	t.Helper()
	p, err := c.tmpPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCacheRejectsPathEscape(t *testing.T) {
	c := testCache(t, 1024)
	tmp := writeTemp(t, c, "x")
	if err := c.Put("../outside.txt", tmp, fileMeta{}); err == nil {
		t.Fatal("expected bad key")
	}
	if _, err := os.Stat(filepath.Join(c.Root(), "outside.txt")); err == nil {
		t.Fatal("escaped write")
	}
}

func TestNewCacheSkipsBackendFingerprint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, backendFileName), []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.bin"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewCache(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if c.Used() != 2 {
		t.Fatalf("used = %d, want 2 (fingerprint must not be indexed)", c.Used())
	}
	if _, _, ok := c.Get("keep.bin"); !ok {
		t.Fatal("committed objects must still be indexed")
	}
}

func TestNewCacheRemovesAbandonedTmp(t *testing.T) {
	dir := t.TempDir()
	tmpDir := filepath.Join(dir, tmpDirName)
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(tmpDir, "leftover.part")
	if err := os.WriteFile(part, make([]byte, 200), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(keep, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := NewCache(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf("abandoned tmp must be removed, err=%v", err)
	}
	if _, _, ok := c.Get("keep.bin"); !ok {
		t.Fatal("committed objects must survive tmp cleanup")
	}
}

func TestNewCacheEvictsExistingOverCap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old.bin"), make([]byte, 200), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewCache(dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Get("old.bin"); ok {
		t.Fatal("startup must evict objects that already exceed the cap")
	}
	if c.Used() != 0 {
		t.Fatalf("used = %d, want 0", c.Used())
	}
}

func TestCommitDropsStaleEpoch(t *testing.T) {
	c := testCache(t, 1024)
	epoch := c.Epoch()
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	tmp := writeTemp(t, c, "stale")
	if err := c.commit("a.txt", tmp, fileMeta{}, epoch); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Get("a.txt"); ok {
		t.Fatal("commit started before flush must be discarded")
	}
}

func TestCacheObjectPermissions(t *testing.T) {
	c := testCache(t, 1024)
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}
	path, _, ok := c.Get("a.txt")
	if !ok {
		t.Fatal("expected hit")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("object mode = %o, want 0600", info.Mode().Perm())
	}
}

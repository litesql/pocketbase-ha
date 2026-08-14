package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/litesql/go-ha"
	"github.com/litesql/pocketbase-ha/internal/config"
)

func TestResolveCacheDirDefault(t *testing.T) {
	data := t.TempDir()
	got, err := resolveCacheDir(data, "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(data, localCacheDirName)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveCacheDirRelativeJoinsDataDir(t *testing.T) {
	data := t.TempDir()
	got, err := resolveCacheDir(data, "hot")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(data, "hot")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveCacheDirRejectsDataDirAndStorage(t *testing.T) {
	data := t.TempDir()
	if _, err := resolveCacheDir(data, data); err == nil {
		t.Fatal("data dir must be rejected")
	}
	if _, err := resolveCacheDir(data, filepath.Join(data, "storage")); err == nil {
		t.Fatal("storage/ must be rejected")
	}
	if _, err := resolveCacheDir(data, filepath.Join(data, "storage", "nested")); err == nil {
		t.Fatal("path inside storage/ must be rejected")
	}
}

func TestNewCacheDisabledDoesNotCreateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	c, err := NewCache(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled() {
		t.Fatal("cap 0 must be disabled")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("disabled cache must not mkdir, err=%v", err)
	}
}

func TestIsInside(t *testing.T) {
	parent := filepath.Join(string(os.PathSeparator), "data", "storage")
	child := filepath.Join(parent, "nested")
	if !isInside(child, parent) {
		t.Fatal("nested path should be inside parent")
	}
	if isInside(parent, parent) {
		t.Fatal("path should not be inside itself")
	}
	sibling := filepath.Join(string(os.PathSeparator), "data", "filecache")
	if isInside(sibling, parent) {
		t.Fatal("sibling must not be inside storage")
	}
	if !strings.HasPrefix(filepath.ToSlash(child), filepath.ToSlash(parent)) {
		t.Fatal("sanity")
	}
}

type stubLeader struct {
	target string
}

func (s stubLeader) IsLeader() bool         { return s.target == "" }
func (s stubLeader) Ready() chan struct{}   { return nil }
func (s stubLeader) RedirectTarget() string { return s.target }

type stubLeaders struct {
	lp stubLeader
}

func (s stubLeaders) LeaderProvider() ha.LeaderProvider { return s.lp }

func TestFeatureUsesInjectedLeaderSource(t *testing.T) {
	f := New(config.Config{}, stubLeaders{lp: stubLeader{target: "http://leader:8090"}})
	got := f.leader()
	if got == nil || got.RedirectTarget() != "http://leader:8090" {
		t.Fatalf("leader = %#v", got)
	}
	if New(config.Config{}, nil).leader() != nil {
		t.Fatal("nil source must yield nil leader")
	}
}

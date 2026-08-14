package files

import (
	"os"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

type stubFiles struct {
	path string
}

func (s stubFiles) BaseFilesPath() string { return s.path }

func TestInvalidateModelDeletesPrefix(t *testing.T) {
	c := testCache(t, 1024)
	if err := c.Put("col/rec/a.txt", writeTemp(t, c, "a"), fileMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("col/rec/thumbs_a.txt/100x100_a.txt", writeTemp(t, c, "t"), fileMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("col/other/b.txt", writeTemp(t, c, "b"), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	f := &Feature{cache: c}
	f.invalidateModel(stubFiles{path: "col/rec"})

	if _, _, ok := c.Get("col/rec/a.txt"); ok {
		t.Fatal("record files should be gone")
	}
	if _, _, ok := c.Get("col/rec/thumbs_a.txt/100x100_a.txt"); ok {
		t.Fatal("thumbs should be gone")
	}
	if _, _, ok := c.Get("col/other/b.txt"); !ok {
		t.Fatal("other record should remain")
	}
}

func TestInvalidateSkipsModelsWithoutFileFields(t *testing.T) {
	c := testCache(t, 1024)
	col := core.NewBaseCollection("plain", "plainid")
	key := col.BaseFilesPath() + "/a.txt"
	if err := c.Put(key, writeTemp(t, c, "a"), fileMeta{}); err != nil {
		t.Fatal(err)
	}
	f := &Feature{cache: c}
	f.invalidateModel(col)
	if _, _, ok := c.Get(key); !ok {
		t.Fatal("collection without file fields must not wipe the cache")
	}
}

func TestFlushOnS3Toggle(t *testing.T) {
	c := testCache(t, 1024)
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	f := &Feature{cache: c, lastS3: false, s3Known: true}
	f.applyS3Enabled(true)
	if _, err := os.Stat(c.Root() + "/a.txt"); err == nil {
		t.Fatal("expected flush to remove a.txt")
	}
	f.applyS3Enabled(true)
	if err := c.Put("b.txt", writeTemp(t, c, "y"), fileMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Get("b.txt"); !ok {
		t.Fatal("same-value reload must not flush")
	}
}

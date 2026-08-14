package files

import (
	"os"
	"path/filepath"
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
	if err := writeStorageFingerprint(c.Root(), fingerprintFromS3(core.S3Config{})); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	s3 := core.S3Config{Enabled: true, Bucket: "b", Endpoint: "https://s3.example"}
	f := &Feature{cache: c}
	f.applyStorageBackend(s3)
	if _, err := os.Stat(filepath.Join(c.Root(), "a.txt")); err == nil {
		t.Fatal("expected flush to remove a.txt")
	}
	if err := c.Put("b.txt", writeTemp(t, c, "y"), fileMeta{}); err != nil {
		t.Fatal(err)
	}
	f.applyStorageBackend(s3)
	if _, _, ok := c.Get("b.txt"); !ok {
		t.Fatal("same-value reload must not flush")
	}
}

func TestFlushOnS3BucketChange(t *testing.T) {
	c := testCache(t, 1024)
	s3a := core.S3Config{Enabled: true, Bucket: "a", Endpoint: "https://s3.example"}
	if err := writeStorageFingerprint(c.Root(), fingerprintFromS3(s3a)); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	f := &Feature{cache: c}
	f.applyStorageBackend(core.S3Config{Enabled: true, Bucket: "other", Endpoint: "https://s3.example"})
	if _, _, ok := c.Get("a.txt"); ok {
		t.Fatal("bucket change must flush")
	}
}

func TestFlushOnS3EndpointChange(t *testing.T) {
	c := testCache(t, 1024)
	s3a := core.S3Config{Enabled: true, Bucket: "a", Endpoint: "https://s3.example"}
	if err := writeStorageFingerprint(c.Root(), fingerprintFromS3(s3a)); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	f := &Feature{cache: c}
	f.applyStorageBackend(core.S3Config{Enabled: true, Bucket: "a", Endpoint: "https://other.example"})
	if _, _, ok := c.Get("a.txt"); ok {
		t.Fatal("endpoint change must flush")
	}
}

func TestNoFlushWhenDisabledS3FieldsChange(t *testing.T) {
	c := testCache(t, 1024)
	if err := writeStorageFingerprint(c.Root(), fingerprintFromS3(core.S3Config{})); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	f := &Feature{cache: c}
	f.applyStorageBackend(core.S3Config{Enabled: false, Bucket: "other", Endpoint: "https://s3.example"})
	if _, _, ok := c.Get("a.txt"); !ok {
		t.Fatal("inactive S3 fields must not flush local cache")
	}
}

func TestFlushWhenFingerprintMissingAndCachePopulated(t *testing.T) {
	c := testCache(t, 1024)
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	f := &Feature{cache: c}
	f.applyStorageBackend(core.S3Config{})
	if _, _, ok := c.Get("a.txt"); ok {
		t.Fatal("missing fingerprint with existing objects must flush")
	}
}

func TestSameBackendKeepsCache(t *testing.T) {
	c := testCache(t, 1024)
	s3 := core.S3Config{Enabled: true, Bucket: "b", Endpoint: "https://s3.example"}
	if err := writeStorageFingerprint(c.Root(), fingerprintFromS3(s3)); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("a.txt", writeTemp(t, c, "x"), fileMeta{}); err != nil {
		t.Fatal(err)
	}

	f := &Feature{cache: c}
	f.applyStorageBackend(s3)
	if _, _, ok := c.Get("a.txt"); !ok {
		t.Fatal("matching fingerprint must keep objects")
	}
}

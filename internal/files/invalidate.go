package files

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/pocketbase/pocketbase/core"
)

func (f *Feature) onModelAfterChange(e *core.ModelEvent) error {
	f.invalidateModel(e.Model)
	return e.Next()
}

func (f *Feature) invalidateModel(model any) {
	if f.cache == nil || !f.cache.Enabled() || model == nil {
		return
	}
	m, ok := model.(core.FilesManager)
	if !ok {
		return
	}
	if !modelHasFileFields(model) {
		return
	}
	prefix := m.BaseFilesPath()
	if prefix == "" {
		return
	}
	f.cache.DeletePrefix(prefix)
}

func modelHasFileFields(m any) bool {
	var collection *core.Collection
	switch v := m.(type) {
	case *core.Collection:
		collection = v
	case *core.Record:
		collection = v.Collection()
	case core.RecordProxy:
		if v.ProxyRecord() != nil {
			collection = v.ProxyRecord().Collection()
		}
	}
	if collection == nil {
		return true
	}
	for _, field := range collection.Fields {
		if field.Type() == core.FieldTypeFile {
			return true
		}
	}
	return false
}

func (f *Feature) onSettingsReload(e *core.SettingsReloadEvent) error {
	if err := e.Next(); err != nil {
		return err
	}
	if e.App != nil && e.App.Settings() != nil {
		f.applyStorageBackend(e.App.Settings().S3)
	}
	return nil
}

func (f *Feature) snapshotS3(app core.App) {
	if app == nil || app.Settings() == nil {
		return
	}
	f.applyStorageBackend(app.Settings().S3)
}

// storageFingerprint identifies the file bytes origin. Disabled S3 is
// "local" regardless of leftover bucket/endpoint fields in settings.
type storageFingerprint struct {
	Enabled  bool   `json:"enabled"`
	Bucket   string `json:"bucket,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

func fingerprintFromS3(s3 core.S3Config) storageFingerprint {
	if !s3.Enabled {
		return storageFingerprint{}
	}
	return storageFingerprint{Enabled: true, Bucket: s3.Bucket, Endpoint: s3.Endpoint}
}

func (a storageFingerprint) same(b storageFingerprint) bool {
	if !a.Enabled && !b.Enabled {
		return true
	}
	return a == b
}

func (f *Feature) applyStorageBackend(s3 core.S3Config) {
	if f == nil || f.cache == nil || !f.cache.Enabled() {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	next := fingerprintFromS3(s3)
	prev, ok := readStorageFingerprint(f.cache.Root())
	needFlush := false
	if ok {
		needFlush = !prev.same(next)
	} else {
		needFlush = f.cache.Used() > 0
	}
	if needFlush {
		_ = f.cache.Flush()
	}
	_ = writeStorageFingerprint(f.cache.Root(), next)
}

func readStorageFingerprint(root string) (storageFingerprint, bool) {
	raw, err := os.ReadFile(filepath.Join(root, backendFileName))
	if err != nil {
		return storageFingerprint{}, false
	}
	var fp storageFingerprint
	if err := json.Unmarshal(raw, &fp); err != nil {
		return storageFingerprint{}, false
	}
	return fp, true
}

func writeStorageFingerprint(root string, fp storageFingerprint) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(fp)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, backendFileName), raw, 0o600)
}

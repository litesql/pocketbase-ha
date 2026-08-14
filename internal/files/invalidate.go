package files

import (
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
	enabled := false
	if e.App != nil && e.App.Settings() != nil {
		enabled = e.App.Settings().S3.Enabled
	}
	f.applyS3Enabled(enabled)
	return nil
}

func (f *Feature) applyS3Enabled(enabled bool) {
	f.mu.Lock()
	prev := f.lastS3
	initialized := f.s3Known
	f.lastS3 = enabled
	f.s3Known = true
	f.mu.Unlock()
	if initialized && prev != enabled && f.cache != nil && f.cache.Enabled() {
		_ = f.cache.Flush()
	}
}

func (f *Feature) snapshotS3(app core.App) {
	if app == nil || app.Settings() == nil {
		return
	}
	f.mu.Lock()
	f.lastS3 = app.Settings().S3.Enabled
	f.s3Known = true
	f.mu.Unlock()
}

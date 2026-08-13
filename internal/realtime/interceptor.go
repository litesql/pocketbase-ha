package realtime

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"github.com/litesql/go-ha"
	"github.com/pocketbase/pocketbase/core"
)

var _ ha.ChangeSetInterceptor = (*ChangeSetInterceptor)(nil)

// ChangeSetInterceptor republishes replicated go-ha changes as PocketBase model events.
type ChangeSetInterceptor struct {
	app            core.App
	mu             sync.Mutex
	deletedRecords map[*ha.ChangeSet][]*core.Record
}

func NewInterceptor() *ChangeSetInterceptor {
	return &ChangeSetInterceptor{}
}

func (i *ChangeSetInterceptor) SetApp(app core.App) {
	i.app = app
}

func (i *ChangeSetInterceptor) BeforeApply(cs *ha.ChangeSet, _ *sql.Conn) (skip bool, err error) {
	if i.app == nil {
		return false, nil
	}

	var deletedRecords []*core.Record
	for _, change := range cs.Changes {
		if change.Operation != "DELETE" {
			continue
		}

		record, ok := i.recordFromChange(change, true)
		if !ok {
			continue
		}

		deletedRecords = append(deletedRecords, record)

		// PocketBase prepares realtime delete messages before the row disappears.
		i.triggerModelDelete(record)
	}

	if len(deletedRecords) > 0 {
		i.setDeletedRecords(cs, deletedRecords)
	}

	return false, nil
}

func (i *ChangeSetInterceptor) AfterApply(cs *ha.ChangeSet, _ *sql.Conn, err error) error {
	if i.app == nil {
		return err
	}

	var reloadCollections, reloadSettings bool
	deletedRecords := i.popDeletedRecords(cs)
	deletedIndex := 0

	for _, change := range cs.Changes {
		if change.Table == "_collections" {
			reloadCollections = true
		}
		if change.Table == "_params" {
			reloadSettings = true
		}

		if err != nil {
			m := ModelFromChange(change, err)
			if m != nil {
				m.TriggerAfterEvent(i.app)
			}
			continue
		}

		switch change.Operation {
		case "INSERT":
			if record, ok := i.recordFromChange(change, false); ok {
				i.triggerModelEvent(core.ModelEventTypeCreate, record)
				continue
			}
		case "UPDATE":
			if record, ok := i.recordFromChange(change, false); ok {
				i.triggerModelEvent(core.ModelEventTypeUpdate, record)
				continue
			}
		case "DELETE":
			if deletedIndex < len(deletedRecords) {
				i.triggerModelEvent(core.ModelEventTypeDelete, deletedRecords[deletedIndex])
				deletedIndex++
				continue
			}
		}

		m := ModelFromChange(change, nil)
		if m != nil {
			m.TriggerAfterEvent(i.app)
		}
	}
	if err == nil {
		if reloadCollections {
			i.app.ReloadCachedCollections()
		}
		if reloadSettings {
			i.app.ReloadSettings()
		}
	}
	return err
}

func (i *ChangeSetInterceptor) recordFromChange(change ha.Change, old bool) (*core.Record, bool) {
	id, ok := recordIDFromChange(change, old)
	if !ok {
		return nil, false
	}

	collection, err := i.app.FindCachedCollectionByNameOrId(change.Table)
	if err != nil {
		return nil, false
	}

	record, err := i.app.FindRecordById(collection, id)
	if err != nil {
		return nil, false
	}

	return record, true
}

func (i *ChangeSetInterceptor) triggerModelEvent(eventType string, model core.Model) {
	event := new(core.ModelEvent)
	event.App = i.app
	event.Context = context.Background()
	event.Type = eventType
	event.Model = model

	var err error
	switch eventType {
	case core.ModelEventTypeCreate:
		err = i.app.OnModelAfterCreateSuccess().Trigger(event)
	case core.ModelEventTypeUpdate:
		err = i.app.OnModelAfterUpdateSuccess().Trigger(event)
	case core.ModelEventTypeDelete:
		err = i.app.OnModelAfterDeleteSuccess().Trigger(event)
	}
	if err != nil {
		slog.Warn("failed to trigger replicated model event", "type", eventType, "table", model.TableName(), "pk", model.PK(), "error", err)
	}
}

func (i *ChangeSetInterceptor) triggerModelDelete(model core.Model) {
	event := new(core.ModelEvent)
	event.App = i.app
	event.Context = context.Background()
	event.Type = core.ModelEventTypeDelete
	event.Model = model

	if err := i.app.OnModelDelete().Trigger(event); err != nil {
		slog.Warn("failed to trigger replicated model delete event", "table", model.TableName(), "pk", model.PK(), "error", err)
	}
}

func (i *ChangeSetInterceptor) setDeletedRecords(cs *ha.ChangeSet, records []*core.Record) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.deletedRecords == nil {
		i.deletedRecords = make(map[*ha.ChangeSet][]*core.Record)
	}
	i.deletedRecords[cs] = records
}

func (i *ChangeSetInterceptor) popDeletedRecords(cs *ha.ChangeSet) []*core.Record {
	i.mu.Lock()
	defer i.mu.Unlock()

	records := i.deletedRecords[cs]
	delete(i.deletedRecords, cs)
	return records
}

func recordIDFromChange(change ha.Change, old bool) (string, bool) {
	value, ok := changePKValue(change, old)
	if !ok {
		return "", false
	}

	id := fmt.Sprint(value)
	return id, id != ""
}

func changePKValue(change ha.Change, old bool) (any, bool) {
	values := change.PKNewValues()
	if old {
		values = change.PKOldValues()
	}
	if len(values) == 0 || values[0] == nil {
		return nil, false
	}
	return values[0], true
}

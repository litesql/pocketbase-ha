package realtime

import (
	"context"

	"github.com/litesql/go-ha"
	"github.com/pocketbase/pocketbase/core"
)

var _ core.Model = &Model{}

type Model struct {
	tableName string
	pk        any
	oldPk     any
	new       bool
	eventType string
	err       error
}

func ModelFromChange(c ha.Change, err error) *Model {
	var m Model
	switch c.Operation {
	case "INSERT":
		m.new = true
		m.eventType = core.ModelEventTypeCreate
	case "UPDATE":
		m.oldPk, _ = changePKValue(c, true)
		m.eventType = core.ModelEventTypeUpdate
	case "DELETE":
		m.oldPk, _ = changePKValue(c, true)
		m.eventType = core.ModelEventTypeDelete
	default:
		return nil
	}
	m.tableName = c.Table
	if m.eventType == core.ModelEventTypeDelete {
		m.pk, _ = changePKValue(c, true)
	} else {
		m.pk, _ = changePKValue(c, false)
	}
	if m.pk == nil {
		return nil
	}
	m.err = err
	return &m
}

func (m *Model) TableName() string {
	return m.tableName
}

func (m *Model) PK() any {
	return m.pk
}

func (m *Model) LastSavedPK() any {
	return m.oldPk
}

func (m *Model) IsNew() bool {
	return m.new
}

func (m *Model) MarkAsNew() {
	m.oldPk = nil
	m.new = true
}

func (m *Model) MarkAsNotNew() {
	m.oldPk = m.pk
	m.new = false
}

func (m *Model) TriggerAfterEvent(app core.App) {
	event := new(core.ModelEvent)
	event.App = app
	event.Context = context.Background()
	event.Type = m.eventType
	event.Model = m
	switch m.eventType {
	case core.ModelEventTypeCreate:
		m.triggerAfterCreate(app, event)
	case core.ModelEventTypeUpdate:
		m.triggerAfterUpdate(app, event)
	case core.ModelEventTypeDelete:
		m.triggerAfterDelete(app, event)
	}
}

func (m *Model) triggerAfterCreate(app core.App, event *core.ModelEvent) {
	if m.err != nil {
		app.OnModelAfterCreateError().Trigger(&core.ModelErrorEvent{
			ModelEvent: *event,
			Error:      m.err,
		})
		return
	}
	app.OnModelAfterCreateSuccess().Trigger(event)
}

func (m *Model) triggerAfterUpdate(app core.App, event *core.ModelEvent) {
	if m.err != nil {
		app.OnModelAfterUpdateError().Trigger(&core.ModelErrorEvent{
			ModelEvent: *event,
			Error:      m.err,
		})
		return
	}
	app.OnModelAfterUpdateSuccess().Trigger(event)
}

func (m *Model) triggerAfterDelete(app core.App, event *core.ModelEvent) {
	if m.err != nil {
		app.OnModelAfterDeleteError().Trigger(&core.ModelErrorEvent{
			ModelEvent: *event,
			Error:      m.err,
		})
		return
	}
	app.OnModelAfterDeleteSuccess().Trigger(event)
}

package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strconv"

	"github.com/litesql/go-ha"
	sqliteha "github.com/litesql/go-sqlite-ha"
	"github.com/litesql/sqlite"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

var (
	bootstrap   = make(chan struct{})
	interceptor = new(ChangeSetInterceptor)
)

func init() {
	drv := sqliteha.Driver{
		ConnectionHook: func(conn sqlite.ExecQuerierContext, dsn string) error {
			_, err := conn.ExecContext(context.Background(), `
			PRAGMA busy_timeout       = 10000;
			PRAGMA journal_mode       = WAL;
			PRAGMA journal_size_limit = 200000000;
			PRAGMA synchronous        = NORMAL;
			PRAGMA foreign_keys       = ON;
			PRAGMA temp_store         = MEMORY;
			PRAGMA cache_size         = -16000;
		`, nil)

			return err
		},
		Options: []ha.Option{
			ha.WithName(os.Getenv("PB_NAME")),
			ha.WithReplicationURL(os.Getenv("PB_REPLICATION_URL")),
			ha.WithWaitFor(bootstrap),
			ha.WithChangeSetInterceptor(interceptor),
		},
	}

	if async := os.Getenv("PB_ASYNC_PUBLISHER"); async != "" {
		b, err := strconv.ParseBool(async)
		if err != nil {
			log.Fatalf("invalid PB_ASYNC_PUBLISHER: %v", err)
		}
		if b {
			drv.Options = append(drv.Options, ha.WithAsyncPublisher(),
				ha.WithAsyncPublisherOutboxDir(os.Getenv("PB_ASYNC_PUBLISHER_DIR")))
		}
	}

	stream := os.Getenv("PB_REPLICATION_STREAM")
	if stream == "" {
		stream = "pb"
	}
	drv.Options = append(drv.Options, ha.WithReplicationStream(stream))

	var embeddedNatsConfig *ha.EmbeddedNatsConfig
	if natsConfigFile := os.Getenv("PB_NATS_CONFIG"); natsConfigFile != "" {
		embeddedNatsConfig = &ha.EmbeddedNatsConfig{
			File: natsConfigFile,
		}
	} else if natsPort := os.Getenv("PB_NATS_PORT"); natsPort != "" {
		port, err := strconv.Atoi(natsPort)
		if err != nil {
			panic("invalid PB_NATS_PORT value:" + err.Error())
		}
		embeddedNatsConfig = &ha.EmbeddedNatsConfig{
			Port:     port,
			StoreDir: os.Getenv("PB_NATS_STORE_DIR"),
		}
	}
	if replicas := os.Getenv("PB_REPLICAS"); replicas != "" {
		replicasInt, err := strconv.Atoi(replicas)
		if err != nil {
			panic("invalid PB_REPLICAS value:" + err.Error())
		}
		drv.Options = append(drv.Options, ha.WithReplicas(replicasInt))
	}
	drv.Options = append(drv.Options, ha.WithEmbeddedNatsConfig(embeddedNatsConfig))

	rowIdentify := os.Getenv("PB_ROW_IDENTIFY")
	if rowIdentify != "" {
		switch rowIdentify {
		case string(ha.Rowid):
			drv.Options = append(drv.Options, ha.WithRowIdentify(ha.Rowid))
		case string(ha.Full):
			drv.Options = append(drv.Options, ha.WithRowIdentify(ha.Full))
		default:
			panic("invaid PB_ROW_IDENTIFY: " + rowIdentify)
		}
	}

	sql.Register("pb_ha", &drv)

	dbx.BuilderFuncMap["pb_ha"] = dbx.BuilderFuncMap["sqlite"]
}

func main() {
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DBConnect: func(dbPath string) (*dbx.DB, error) {
			return dbx.Open("pb_ha", dbPath)
		},
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		close(bootstrap)
		return e.Next()
	})

	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		ha.Shutdown()
		return e.Next()
	})

	interceptor.app = app
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

type ChangeSetInterceptor struct {
	app core.App
}

func (i *ChangeSetInterceptor) BeforeApply(cs *ha.ChangeSet, _ *sql.Conn) (skip bool, err error) {
	for _, change := range cs.Changes {
		if change.Table == "_authOrigins" {
			return true, nil
		}
	}
	return false, nil
}

func (i *ChangeSetInterceptor) AfterApply(cs *ha.ChangeSet, _ *sql.Conn, err error) error {
	var reloadCollections, reloadSettings bool
	for _, change := range cs.Changes {
		if change.Table == "_collections" {
			reloadCollections = true
		}
		if change.Table == "_params" {
			reloadSettings = true
		}
		m := ModelFromChange(change, err)
		if m == nil {
			continue
		}
		m.TriggerAfterEvent(i.app)
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
		m.oldPk = c.OldRowID
		m.eventType = core.ModelEventTypeUpdate
	case "DELETE":
		m.oldPk = c.OldRowID
		m.eventType = core.ModelEventTypeDelete
	default:
		return nil
	}
	m.tableName = c.Table
	m.pk = c.NewRowID
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

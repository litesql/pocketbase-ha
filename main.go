package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

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
	if leader := os.Getenv("PB_STATIC_LEADER"); leader != "" {
		drv.Options = append(drv.Options, ha.WithLeaderProvider(&ha.StaticLeader{
			Target: leader,
		}))
	}
	if redirect := os.Getenv("PB_LOCAL_TARGET"); redirect != "" {
		drv.Options = append(drv.Options, ha.WithLeaderElectionLocalTarget(redirect))
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

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		close(bootstrap)

		// force sync token definition
		_, err := app.ConcurrentDB().Update("_collections",
			dbx.Params{"updated": time.Now().Format("2006-01-02 15:04:05.000Z")},
			dbx.In("name", "_superusers", "users")).Execute()
		if err != nil {
			return fmt.Errorf("failed to sync configure: %w", err)
		}

		superuserEmail := os.Getenv("PB_SUPERUSER_EMAIL")
		superuserPass := os.Getenv("PB_SUPERUSER_PASS")
		if superuserEmail != "" && superuserPass != "" {

			superusersCol, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
			if err != nil {
				return fmt.Errorf("failed to fetch %q collection: %w", core.CollectionNameSuperusers, err)
			}

			superuser, err := app.FindAuthRecordByEmail(superusersCol, superuserEmail)
			if err != nil {
				superuser = core.NewRecord(superusersCol)
			}

			superuser.SetEmail(superuserEmail)
			superuser.SetPassword(superuserPass)

			if err := app.Save(superuser); err != nil {
				return fmt.Errorf("failed to set superuser account: %w", err)
			}
		}

		var dataDSN string
		for _, dsn := range ha.ListDSN() {
			if strings.HasSuffix(dsn, "data.db") {
				dataDSN = dsn
				break
			}
		}

		connector, ok := ha.LookupConnector(dataDSN)
		if !ok {
			return fmt.Errorf("connector not found")
		}
		slog.Info("waiting for the leader")
		<-connector.LeaderProvider().Ready()

		timeout := 10 * time.Second

		se.Router.BindFunc(func(e *core.RequestEvent) error {
			if connector.LeaderProvider().IsLeader() ||
				strings.Contains(e.Request.URL.Path, "/auth-with-password") ||
				strings.Contains(e.Request.URL.Path, "/auth-refresh") {
				if e.Request.Method != "GET" {
					e.Response = connector.ResponseWriter(e.Response)
				}
				return e.Next()
			}

			switch e.Request.Method {
			case "POST", "PUT", "PATCH", "DELETE":
				target := connector.LeaderProvider().RedirectTarget()
				if target == "" {
					return e.InternalServerError("leader redirect URL not found", nil)
				}
				resp, err := forwardTo(target, e.Request, timeout)
				if err != nil {
					return e.InternalServerError(err.Error(), nil)
				}
				defer resp.Body.Close()
				for k, v := range resp.Header {
					for i, value := range v {
						if i == 0 {
							e.Response.Header().Set(k, value)
							continue
						}
						e.Response.Header().Add(k, value)
					}
				}
				e.Response.WriteHeader(resp.StatusCode)
				io.Copy(e.Response, resp.Body)
				return nil
			case "GET":
				var txSeq uint64
				if cookie, _ := e.Request.Cookie(ha.TXCookieName); cookie != nil {
					var err error
					txSeq, err = strconv.ParseUint(cookie.Value, 10, 64)
					if err != nil {
						slog.Warn("invalid cookie", "name", ha.TXCookieName, "error", err)
						return e.Next()
					}
				}

				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()

				ctx, cancel := context.WithTimeout(e.Request.Context(), timeout)
				defer cancel()
			LOOP:
				for {
					if connector.LatestSeq() >= txSeq {
						break LOOP
					}

					select {
					case <-ctx.Done():
						target := connector.LeaderProvider().RedirectTarget()
						if target == "" {
							return e.InternalServerError("leader redirect URL not found", nil)
						}
						resp, err := forwardTo(target, e.Request, timeout)
						if err != nil {
							return e.InternalServerError(err.Error(), nil)
						}
						defer resp.Body.Close()
						for k, v := range resp.Header {
							for i, value := range v {
								if i == 0 {
									e.Response.Header().Set(k, value)
									continue
								}
								e.Response.Header().Add(k, value)
							}
						}
						e.Response.WriteHeader(resp.StatusCode)
						io.Copy(e.Response, resp.Body)
						return nil
					case <-ticker.C:
					}
				}
			}

			return e.Next()

		})

		return se.Next()
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

func forwardTo(addr string, req *http.Request, timeout time.Duration) (*http.Response, error) {
	newURL := addr + req.URL.Path + "?" + req.URL.RawQuery

	var buf bytes.Buffer
	defer req.Body.Close()
	_, err := io.Copy(&buf, req.Body)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(req.Context(), timeout)
	defer cancel()
	newReq, err := http.NewRequestWithContext(ctx, req.Method, newURL, &buf)
	if err != nil {
		return nil, err
	}
	for k, v := range req.Header {
		for i, value := range v {
			if i == 0 {
				newReq.Header.Set(k, value)
				continue
			}
			newReq.Header.Add(k, value)
		}
	}
	return http.DefaultClient.Do(newReq)
}

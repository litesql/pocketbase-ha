package cluster

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/litesql/go-ha"
	"github.com/litesql/pocketbase-ha/internal/config"
	"github.com/litesql/pocketbase-ha/internal/feature"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const writeForwardTimeout = 10 * time.Second

var _ feature.Feature = (*Feature)(nil)

// Feature waits for the cluster leader, bootstraps the superuser, and binds HA HTTP routing.
type Feature struct {
	cfg       config.Config
	bootstrap chan struct{}

	mu     sync.Mutex
	leader ha.LeaderProvider
}

func New(cfg config.Config, bootstrap chan struct{}) *Feature {
	return &Feature{cfg: cfg, bootstrap: bootstrap}
}

func (f *Feature) Name() string {
	return "cluster"
}

func (f *Feature) Register(app core.App) error {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		close(f.bootstrap)

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
		leader := connector.LeaderProvider()
		f.setLeader(leader)
		slog.Info("waiting for the leader")
		<-leader.Ready()

		if leader.IsLeader() {
			// force sync token definition
			_, err := app.ConcurrentDB().Update("_collections",
				dbx.Params{"updated": time.Now().Format("2006-01-02 15:04:05.000Z")},
				dbx.In("name", "_superusers", "users")).Execute()
			if err != nil {
				return fmt.Errorf("failed to sync configure: %w", err)
			}
		}

		if err := upsertSuperuser(app, f.cfg.SuperuserEmail, f.cfg.SuperuserPass); err != nil {
			return err
		}

		forwardWrites := keepRealtimeLocal(connector.ForwardToLeader(writeForwardTimeout, "POST", "PUT", "PATCH", "DELETE"))
		se.Router.BindFunc(apis.WrapStdMiddleware(forwardWrites))
		se.Router.BindFunc(apis.WrapStdMiddleware(connector.ConsistentReader(writeForwardTimeout, "GET")))
		return se.Next()
	})
	return nil
}

// LeaderProvider returns the data.db leader once OnServe has resolved it.
func (f *Feature) LeaderProvider() ha.LeaderProvider {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leader
}

func (f *Feature) setLeader(leader ha.LeaderProvider) {
	f.mu.Lock()
	f.leader = leader
	f.mu.Unlock()
}

func upsertSuperuser(app core.App, email, pass string) error {
	if email == "" || pass == "" {
		return nil
	}

	superusersCol, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return fmt.Errorf("failed to fetch %q collection: %w", core.CollectionNameSuperusers, err)
	}

	superuser, err := app.FindAuthRecordByEmail(superusersCol, email)
	if err != nil {
		superuser = core.NewRecord(superusersCol)
	}

	superuser.SetEmail(email)
	superuser.SetPassword(pass)

	if err := app.Save(superuser); err != nil {
		return fmt.Errorf("failed to set superuser account: %w", err)
	}
	return nil
}

func keepRealtimeLocal(middleware func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		forwarded := middleware(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strings.TrimRight(r.URL.Path, "/") == "/api/realtime" {
				next.ServeHTTP(w, r)
				return
			}

			forwarded.ServeHTTP(w, r)
		})
	}
}

package hadriver

import (
	"database/sql"
	"sync"

	"github.com/litesql/go-ha"
	"github.com/litesql/pocketbase-ha/internal/config"
	"github.com/pocketbase/dbx"
)

const DriverName = "pb_ha"

var registerOnce sync.Once

// Register installs the pb_ha SQL driver using cfg, interceptor, and the serve bootstrap gate.
func Register(cfg config.Config, interceptor ha.ChangeSetInterceptor, bootstrap chan struct{}) {
	drv.Options = Options(cfg, interceptor, bootstrap)
	registerOnce.Do(func() {
		sql.Register(DriverName, &drv)
		dbx.BuilderFuncMap[DriverName] = dbx.BuilderFuncMap["sqlite"]
	})
}

// Options builds go-ha driver options from cfg. Order matches the historical main.go init().
func Options(cfg config.Config, interceptor ha.ChangeSetInterceptor, bootstrap chan struct{}) []ha.Option {
	opts := []ha.Option{
		ha.WithName(cfg.Name),
		ha.WithReplicationURL(cfg.ReplicationURL),
		ha.WithWaitFor(bootstrap),
		ha.WithChangeSetInterceptor(interceptor),
	}

	if cfg.AsyncPublisher {
		opts = append(opts, ha.WithAsyncPublisher(),
			ha.WithAsyncPublisherOutboxDir(cfg.AsyncPublisherDir))
	}

	opts = append(opts, ha.WithReplicationStream(cfg.ReplicationStream))

	if cfg.Replicas != nil {
		opts = append(opts, ha.WithReplicas(*cfg.Replicas))
	}

	var embeddedNatsConfig *ha.EmbeddedNatsConfig
	if cfg.EmbeddedNATS != nil {
		embeddedNatsConfig = &ha.EmbeddedNatsConfig{
			File:     cfg.EmbeddedNATS.File,
			Port:     cfg.EmbeddedNATS.Port,
			StoreDir: cfg.EmbeddedNATS.StoreDir,
		}
	}
	opts = append(opts, ha.WithEmbeddedNatsConfig(embeddedNatsConfig))

	if cfg.RowIdentify != "" {
		opts = append(opts, ha.WithRowIdentify(ha.RowIdentify(cfg.RowIdentify)))
	}
	if cfg.StaticLeader != "" {
		opts = append(opts, ha.WithLeaderProvider(&ha.StaticLeader{
			Target: cfg.StaticLeader,
		}))
	}
	if cfg.LocalTarget != "" {
		opts = append(opts, ha.WithLeaderElectionLocalTarget(cfg.LocalTarget))
	}

	if cfg.GRPCPort != nil {
		opts = append(opts, ha.WithGrpcPort(*cfg.GRPCPort))
	}
	if cfg.GRPCToken != "" {
		opts = append(opts, ha.WithGrpcToken(cfg.GRPCToken))
	}

	opts = append(opts, ha.WithStreamMaxAge(cfg.StreamMaxAge))
	return opts
}

package server

import (
	"os"
	"path/filepath"

	"github.com/litesql/go-ha"
	"github.com/litesql/pocketbase-ha/internal/cluster"
	"github.com/litesql/pocketbase-ha/internal/config"
	"github.com/litesql/pocketbase-ha/internal/feature"
	"github.com/litesql/pocketbase-ha/internal/hadriver"
	"github.com/litesql/pocketbase-ha/internal/realtime"
	"github.com/litesql/pocketbase-ha/remote"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/ghupdate"
	"github.com/pocketbase/pocketbase/plugins/jsvm"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/osutils"
)

// New constructs the PocketBase app, registers the HA driver, and attaches cluster features.
func New(cfg config.Config) (*pocketbase.PocketBase, error) {
	bootstrap := make(chan struct{})
	interceptor := realtime.NewInterceptor()
	hadriver.Register(cfg, interceptor, bootstrap)

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DBConnect: func(dbPath string) (*dbx.DB, error) {
			return dbx.Open(hadriver.DriverName, dbPath)
		},
	})

	var hooksDir string
	app.RootCmd.PersistentFlags().StringVar(
		&hooksDir,
		"hooksDir",
		"",
		"the directory with the JS app hooks",
	)

	var hooksWatch bool
	app.RootCmd.PersistentFlags().BoolVar(
		&hooksWatch,
		"hooksWatch",
		true,
		"auto restart the app on pb_hooks file change; it has no effect on Windows",
	)

	var hooksPool int
	app.RootCmd.PersistentFlags().IntVar(
		&hooksPool,
		"hooksPool",
		15,
		"the total prewarm goja.Runtime instances for the JS app hooks execution",
	)

	var migrationsDir string
	app.RootCmd.PersistentFlags().StringVar(
		&migrationsDir,
		"migrationsDir",
		"",
		"the directory with the user defined migrations",
	)

	var automigrate bool
	app.RootCmd.PersistentFlags().BoolVar(
		&automigrate,
		"automigrate",
		true,
		"enable/disable auto migrations",
	)

	var publicDir string
	app.RootCmd.PersistentFlags().StringVar(
		&publicDir,
		"publicDir",
		defaultPublicDir(),
		"the directory to serve static files",
	)

	var indexFallback bool
	app.RootCmd.PersistentFlags().BoolVar(
		&indexFallback,
		"indexFallback",
		true,
		"fallback the request to index.html on missing static path, e.g. when pretty urls are used with SPA",
	)

	app.RootCmd.ParseFlags(os.Args[1:])

	jsvm.MustRegister(app, jsvm.Config{
		MigrationsDir: migrationsDir,
		HooksDir:      hooksDir,
		HooksWatch:    hooksWatch,
		HooksPoolSize: hooksPool,
	})

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		TemplateLang: migratecmd.TemplateLangJS,
		Automigrate:  automigrate,
		Dir:          migrationsDir,
	})

	ghupdate.MustRegister(app, app.RootCmd, ghupdate.Config{
		Owner:             "litesql",
		Repo:              "pocketbase-ha",
		ArchiveExecutable: "pocketbase-ha",
	})

	if err := remote.Register(app.RootCmd); err != nil {
		return nil, err
	}

	interceptor.SetApp(app)

	features := []feature.Feature{
		cluster.New(cfg, bootstrap),
		// later: files.New(cfg), backup.New(cfg)
	}
	for _, f := range features {
		if err := f.Register(app); err != nil {
			return nil, err
		}
	}

	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		ha.Shutdown()
		return e.Next()
	})

	return app, nil
}

func defaultPublicDir() string {
	if osutils.IsProbablyGoRun() {
		return "./pb_public"
	}

	return filepath.Join(os.Args[0], "../pb_public")
}

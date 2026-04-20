//go:build !cgo

package main

import (
	"context"

	sqliteha "github.com/litesql/go-sqlite-ha"
	"modernc.org/sqlite"
)

var drv = sqliteha.Driver{
	ConnectionHook: func(conn sqlite.ExecQuerierContext, dsn string) error {
		_, err := conn.ExecContext(context.Background(), `
			PRAGMA busy_timeout       = 10000;
			PRAGMA journal_mode       = WAL;
			PRAGMA journal_size_limit = 200000000;
			PRAGMA synchronous        = NORMAL;
			PRAGMA foreign_keys       = ON;
			PRAGMA temp_store         = MEMORY;
			PRAGMA cache_size         = -32000;
		`, nil)

		return err
	},
}

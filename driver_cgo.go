//go:build cgo

package main

import (
	"github.com/litesql/go-sqlite3"
	sqlite3ha "github.com/litesql/go-sqlite3-ha"
)

var drv = sqlite3ha.Driver{
	ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		_, err := conn.Exec(`
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

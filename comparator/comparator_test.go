package comparator_test

import (
	"github.com/teandresmith/sqlgen/sql"
)

var (
	pgDialect     = sql.NewPostgresDialect()
	pgStdDialect  = sql.NewPostgresStdlibDialect()
	mysqlDialect  = sql.NewMySQLDialect()
	sqliteDialect = sql.NewSQLiteDialect()
)

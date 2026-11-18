package pgx

import (
	"github.com/jackc/pgx/v5"

	"github.com/teandresmith/sqlgen/database"
)

// MapErrorCtx exposes mapErrorCtx to the external test package.
var MapErrorCtx = mapErrorCtx

// NewTxConn wraps tx in the adapter's database.TxConn, so a test can drive the
// adapter's Commit and Rollback on a pgx transaction it has already ended.
func NewTxConn(tx pgx.Tx) database.TxConn { return &txConn{tx: tx} }

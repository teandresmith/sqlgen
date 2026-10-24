package models

import (
	"time"

	"github.com/teandresmith/sqlgen/types"
)

// Timestamp is the Go type this dialect generates for a timestamp column.
type Timestamp = types.DateTime

// Now returns the current time as a Timestamp.
func Now() Timestamp { return types.DateTime{Time: time.Now().UTC().Truncate(time.Second)} }

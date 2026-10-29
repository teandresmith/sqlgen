package models

import "time"

// Timestamp is the Go type this dialect generates for a timestamp column.
type Timestamp = time.Time

// Now returns the current time as a Timestamp.
func Now() Timestamp { return time.Now().UTC().Truncate(time.Microsecond) }

// DeletedAt is t as a value of this dialect's nullable soft-delete column.
func DeletedAt(t Timestamp) *time.Time { return &t }

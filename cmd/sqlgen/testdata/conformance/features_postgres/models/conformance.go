package models

import (
	"time"
	"uuid"
)

// Timestamp is the Go type this dialect generates for a timestamp column.
type Timestamp = time.Time

// Now returns the current time as a Timestamp.
func Now() Timestamp { return time.Now().UTC().Truncate(time.Microsecond) }

// DeletedAt is t as a value of this dialect's nullable soft-delete column.
func DeletedAt(t Timestamp) *time.Time { return &t }

// Tenant is the Go type this dialect generates for the tenant column.
type Tenant = uuid.UUID

// NewTenant returns a fresh tenant.
func NewTenant() Tenant { return uuid.New() }

package models

import (
	"time"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
)

// Timestamp is the Go type this dialect generates for a timestamp column.
type Timestamp = time.Time

// Now returns the current time as a Timestamp.
func Now() Timestamp { return time.Now().UTC().Truncate(time.Microsecond) }

// DeletedAt is t as a value of this dialect's nullable soft-delete column.
func DeletedAt(t Timestamp) *time.Time { return &t }

// Tenant is the Go type this dialect generates for the tenant column: a
// uuid.UUID tenant on a CHAR column fails the PRD §29.2.4 SQL-type check.
type Tenant = string

// NewTenant returns a fresh tenant.
func NewTenant() Tenant { return uuid.New().String() }

// TenantEq is the filter on a tenant column outside the primary key that
// selects tenant: a string tenant filters through comparator.String.
func TenantEq(tenant Tenant) *comparator.String { return &comparator.String{Eq: new(tenant)} }

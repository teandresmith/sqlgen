package models

import (
	"time"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/types"
)

// Timestamp is the Go type this dialect generates for a timestamp column.
type Timestamp = types.DateTime

// Now returns the current time as a Timestamp.
func Now() Timestamp { return types.DateTime{Time: time.Now().UTC().Truncate(time.Second)} }

// DeletedAt is t as a value of this dialect's nullable soft-delete column.
func DeletedAt(t Timestamp) types.NullDateTime { return types.NullDateTime{Time: t.Time, Valid: true} }

// Tenant is the Go type this dialect generates for the tenant column.
type Tenant = uuid.UUID

// NewTenant returns a fresh tenant.
func NewTenant() Tenant { return uuid.New() }

// TenantEq is the filter on a tenant column outside the primary key that
// selects tenant: a uuid.UUID tenant filters through comparator.ID.
func TenantEq(tenant Tenant) *comparator.ID { return &comparator.ID{Eq: new(tenant.String())} }

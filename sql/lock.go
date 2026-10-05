package sql

// LockMode selects a row-level locking clause appended to SELECT statements.
//
// LockMode applies to read methods only (Get, GetMany, Connection). Write
// methods acquire row locks implicitly via the INSERT/UPDATE/DELETE itself.
//
// LockMode is meaningful only inside an active transaction; the generated read
// methods enforce this precondition via [database.InTransaction]. SQLite has no
// per-row locking and rejects any non-[LockNone] mode at the same precondition
// check — no SQL is issued.
//
// See PRD §9.6a for the full semantic contract (transaction precondition, lock
// release semantics, hook chain interaction, chained-Get exclusion, MySQL
// version requirements for NoWait/SkipLocked).
type LockMode int

const (
	// LockNone emits no lock clause. This is the default.
	LockNone LockMode = iota

	// LockForUpdate emits "FOR UPDATE" — exclusive lock; blocks other readers
	// and writers of the selected rows until the enclosing transaction commits
	// or rolls back.
	LockForUpdate

	// LockForShare emits "FOR SHARE" on PostgreSQL or "LOCK IN SHARE MODE" on
	// MySQL — shared lock; allows other shared readers but blocks writers.
	LockForShare

	// LockForUpdateNoWait emits "FOR UPDATE NOWAIT" — errors immediately if any
	// conflicting lock is held instead of waiting. Requires MySQL 8.0+.
	LockForUpdateNoWait

	// LockForUpdateSkipLocked emits "FOR UPDATE SKIP LOCKED" — skips rows
	// currently locked by another transaction instead of waiting. Suitable for
	// concurrent job-queue workers. Requires MySQL 8.0+.
	LockForUpdateSkipLocked
)

// String returns the canonical name for the lock mode (e.g. "for_update").
// Useful for logs and error messages.
func (m LockMode) String() string {
	switch m {
	case LockNone:
		return "none"
	case LockForUpdate:
		return "for_update"
	case LockForShare:
		return "for_share"
	case LockForUpdateNoWait:
		return "for_update_nowait"
	case LockForUpdateSkipLocked:
		return "for_update_skip_locked"
	default:
		return "unknown"
	}
}

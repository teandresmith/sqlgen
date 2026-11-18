package stdlib_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/database"
	stdlibadapter "github.com/teandresmith/sqlgen/database/stdlib"
)

// holdWindow is how long a statement from a second goroutine must stay
// blocked while the first goroutine holds an open result set. With the
// reservation it waits until the result set is released however long that
// takes, so the window only decides how long the test looks for a statement
// that ran when it should have waited.
const holdWindow = 500 * time.Millisecond

// dialect is one database/sql backend with the SQL a test needs to spell
// differently on it.
type dialect struct {
	db     *sql.DB
	insert string // INSERT INTO test_users (name, email)
	count  string // SELECT COUNT(*) FROM test_users WHERE email = ?
}

func mysqlDialect() dialect {
	return dialect{
		db:     mysqlDB,
		insert: "INSERT INTO test_users (name, email) VALUES (?, ?)",
		count:  "SELECT COUNT(*) FROM test_users WHERE email = ?",
	}
}

func sqliteDialect() dialect {
	return dialect{
		db:     sqliteDB,
		insert: "INSERT INTO test_users (name, email) VALUES (?, ?)",
		count:  "SELECT COUNT(*) FROM test_users WHERE email = ?",
	}
}

func postgresDialect() dialect {
	return dialect{
		db:     postgresDB,
		insert: "INSERT INTO test_users (name, email) VALUES ($1, $2)",
		count:  "SELECT COUNT(*) FROM test_users WHERE email = $1",
	}
}

func (d dialect) rows(t *testing.T, email string) int {
	t.Helper()
	var n int
	if err := d.db.QueryRowContext(context.Background(), d.count, email).Scan(&n); err != nil {
		t.Fatalf("%s: %v", d.count, err)
	}
	return n
}

// openWith opens a one-connection pool on dsn with the extra DSN parameter
// param=value, so every session in it starts with that setting.
func openWith(t *testing.T, driver, dsn, param, value string) *sql.DB {
	t.Helper()
	u := dsn + "&" + param + "=" + url.QueryEscape(value)
	db, err := sql.Open(driver, u)
	if err != nil {
		t.Fatalf("opening %s pool: %v", driver, err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// mysqlObservedIsolation reports which isolation level a MySQL transaction
// begun with level behaves as. @@transaction_isolation reports the session's
// level, not the one SET TRANSACTION gave this transaction, so the level is
// read off InnoDB's behavior instead, one property per level, through a
// second session on mysqlDB:
//   - SERIALIZABLE: the transaction's plain SELECT took a shared lock, so the
//     second session cannot update the row it read.
//   - READ UNCOMMITTED: the transaction sees the second session's uncommitted
//     write.
//   - READ COMMITTED: the transaction sees the second session's committed
//     write on a re-read.
//   - REPEATABLE READ: it sees none of them.
func mysqlObservedIsolation(t *testing.T, q database.Querier, level database.TxIsoLevel) string {
	t.Helper()
	ctx := context.Background()
	cleanTables(t, mysqlDB)
	if _, err := mysqlDB.ExecContext(ctx, "INSERT INTO test_users (id, name, email) VALUES (1, 'orig', 'iso@example.com')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	txCtx, err := database.NewTransaction(ctx, q, "iso", database.TxOptions{IsoLevel: level})
	if err != nil {
		t.Fatalf("NewTransaction(%q) error = %v", level, err)
	}
	defer func() { _ = database.Rollback(txCtx) }()
	tx := database.FromContext(txCtx)
	read := func() string {
		t.Helper()
		var name string
		if err := tx.QueryRow(txCtx, "SELECT name FROM test_users WHERE id = 1").Scan(&name); err != nil {
			t.Fatalf("reading in the transaction: %v", err)
		}
		return name
	}
	if got := read(); got != "orig" {
		t.Fatalf("first read = %q, want %q", got, "orig")
	}

	other, err := mysqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("second session: %v", err)
	}
	defer func() { _ = other.Close() }()
	if _, err := other.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout = 1"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = other.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout = DEFAULT") }()

	if _, err := other.ExecContext(ctx, "START TRANSACTION"); err != nil {
		t.Fatal(err)
	}
	_, err = other.ExecContext(ctx, "UPDATE test_users SET name = 'dirty' WHERE id = 1")
	if mysqlErrNumber(1205)(err) { // lock wait timeout: the read holds a shared lock
		_, _ = other.ExecContext(ctx, "ROLLBACK")
		return "SERIALIZABLE"
	}
	if err != nil {
		t.Fatalf("second session's UPDATE: %v", err)
	}
	dirty := read() == "dirty"
	if _, err := other.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if dirty {
		return "READ-UNCOMMITTED"
	}
	if _, err := other.ExecContext(ctx, "UPDATE test_users SET name = 'committed' WHERE id = 1"); err != nil {
		t.Fatalf("second session's committed UPDATE: %v", err)
	}
	if read() == "committed" {
		return "READ-COMMITTED"
	}
	return "REPEATABLE-READ"
}

func TestTxOptionsIsolationLevel(t *testing.T) {
	requireDocker(t)

	t.Run("mysql", func(t *testing.T) {
		tests := []struct {
			name  string
			level database.TxIsoLevel
			// sessionDefault differs from the level asked for, so the
			// level observed can only come from the transaction's options.
			sessionDefault string
			want           string
		}{
			{name: "serializable", level: database.Serializable, sessionDefault: "'READ-COMMITTED'", want: "SERIALIZABLE"},
			{name: "repeatable read", level: database.RepeatableRead, sessionDefault: "'READ-COMMITTED'", want: "REPEATABLE-READ"},
			{name: "read committed", level: database.ReadCommitted, sessionDefault: "'REPEATABLE-READ'", want: "READ-COMMITTED"},
			{name: "read uncommitted", level: database.ReadUncommitted, sessionDefault: "'REPEATABLE-READ'", want: "READ-UNCOMMITTED"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db := openWith(t, "mysql", mysqlDSN, "transaction_isolation", tt.sessionDefault)
				if got := mysqlObservedIsolation(t, stdlibadapter.New(db), tt.level); got != tt.want {
					t.Errorf("a %q transaction behaves as %s, want %s", tt.level, got, tt.want)
				}
			})
		}
	})

	t.Run("postgres", func(t *testing.T) {
		tests := []struct {
			name           string
			level          database.TxIsoLevel
			sessionDefault string
			want           string
		}{
			{name: "serializable", level: database.Serializable, sessionDefault: "read committed", want: "serializable"},
			{name: "repeatable read", level: database.RepeatableRead, sessionDefault: "serializable", want: "repeatable read"},
			{name: "read committed", level: database.ReadCommitted, sessionDefault: "serializable", want: "read committed"},
			{name: "read uncommitted", level: database.ReadUncommitted, sessionDefault: "serializable", want: "read uncommitted"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db := openWith(t, "postgres", postgresDSN, "default_transaction_isolation", tt.sessionDefault)
				got := showInTx(t, stdlibadapter.New(db), "transaction_isolation", database.TxOptions{IsoLevel: tt.level})
				if got != tt.want {
					t.Errorf("transaction_isolation in a %q transaction = %q, want %q", tt.level, got, tt.want)
				}
			})
		}
	})

	// SQLite transactions are always serializable; any other level is
	// silently ignored, so the transaction begins and works as usual.
	t.Run("sqlite", func(t *testing.T) {
		tests := []struct {
			name  string
			level database.TxIsoLevel
		}{
			{name: "serializable", level: database.Serializable},
			{name: "repeatable read", level: database.RepeatableRead},
			{name: "read committed", level: database.ReadCommitted},
			{name: "read uncommitted", level: database.ReadUncommitted},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assertOptionIgnored(t, sqliteDialect(), database.TxOptions{IsoLevel: tt.level})
			})
		}
	})
}

// assertOptionIgnored begins a transaction with opts, writes in it and
// commits, and checks the write persisted: an option the dialect ignores
// neither fails the BEGIN nor changes what the transaction does.
func assertOptionIgnored(t *testing.T, d dialect, opts database.TxOptions) {
	t.Helper()
	ctx := context.Background()
	cleanTables(t, d.db)
	q := stdlibadapter.New(d.db)
	err := database.WithTransaction(ctx, q, "ignored", func(txCtx context.Context) error {
		_, err := database.Conn(txCtx, q).Exec(txCtx, d.insert, "ignored", "ignored@example.com")
		return err
	}, opts)
	if err != nil {
		t.Fatalf("WithTransaction(%+v) error = %v, want the option ignored", opts, err)
	}
	if got := d.rows(t, "ignored@example.com"); got != 1 {
		t.Errorf("rows after the committed transaction = %d, want 1", got)
	}
}

// showInTx begins a PostgreSQL transaction with opts and returns what SHOW
// setting reports inside it.
func showInTx(t *testing.T, q database.Querier, setting string, opts database.TxOptions) string {
	t.Helper()
	ctx := context.Background()
	txCtx, err := database.NewTransaction(ctx, q, "show", opts)
	if err != nil {
		t.Fatalf("NewTransaction(%+v) error = %v", opts, err)
	}
	defer func() { _ = database.Rollback(txCtx) }()
	var got string
	if err := database.FromContext(txCtx).QueryRow(txCtx, "SHOW "+setting).Scan(&got); err != nil {
		t.Fatalf("SHOW %s inside the transaction: %v", setting, err)
	}
	return got
}

func TestTxOptionsReadOnly(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) {
		err := writeInReadOnlyTx(t, mysqlDialect())
		if !mysqlErrNumber(1792)(err) {
			t.Errorf("INSERT in a ReadOnly transaction error = %v, want ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION (1792)", err)
		}
	})
	t.Run("postgres", func(t *testing.T) {
		err := writeInReadOnlyTx(t, postgresDialect())
		if !pqErrCode("25006")(err) {
			t.Errorf("INSERT in a ReadOnly transaction error = %v, want read_only_sql_transaction (25006)", err)
		}
	})
}

// writeInReadOnlyTx inserts a row inside a ReadOnly transaction, checks that
// nothing was written, and returns the INSERT's error.
func writeInReadOnlyTx(t *testing.T, d dialect) error {
	t.Helper()
	ctx := context.Background()
	cleanTables(t, d.db)
	q := stdlibadapter.New(d.db)
	err := database.WithTransaction(ctx, q, "read_only", func(txCtx context.Context) error {
		_, err := database.Conn(txCtx, q).Exec(txCtx, d.insert, "ro", "ro@example.com")
		return err
	}, database.TxOptions{AccessMode: database.ReadOnly})
	if got := d.rows(t, "ro@example.com"); got != 0 {
		t.Errorf("rows after the ReadOnly transaction = %d, want 0", got)
	}
	return err
}

// TestTxOptionsDeferrable checks that the database/sql adapter ignores
// Deferrable on every dialect: database/sql has no deferrable option, so on
// PostgreSQL the transaction is not deferrable, and elsewhere the option
// changes nothing.
func TestTxOptionsDeferrable(t *testing.T) {
	requireDocker(t)
	opts := database.TxOptions{
		IsoLevel:       database.Serializable,
		AccessMode:     database.ReadOnly,
		DeferrableMode: database.Deferrable,
	}
	t.Run("postgres", func(t *testing.T) {
		if got := showInTx(t, stdlibadapter.New(postgresDB), "transaction_deferrable", opts); got != "off" {
			t.Errorf("transaction_deferrable in a Deferrable transaction = %q, want %q", got, "off")
		}
	})
	t.Run("mysql", func(t *testing.T) {
		assertOptionIgnored(t, mysqlDialect(), database.TxOptions{DeferrableMode: database.Deferrable})
	})
	t.Run("sqlite", func(t *testing.T) {
		assertOptionIgnored(t, sqliteDialect(), database.TxOptions{DeferrableMode: database.Deferrable})
	})
}

func TestRootTransactionCommitAndRollback(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) {
		t.Run("commit persists", func(t *testing.T) { assertRootTeardown(t, mysqlDialect(), database.Commit, 1) })
		t.Run("rollback discards", func(t *testing.T) { assertRootTeardown(t, mysqlDialect(), database.Rollback, 0) })
	})
	t.Run("sqlite", func(t *testing.T) {
		t.Run("commit persists", func(t *testing.T) { assertRootTeardown(t, sqliteDialect(), database.Commit, 1) })
		t.Run("rollback discards", func(t *testing.T) { assertRootTeardown(t, sqliteDialect(), database.Rollback, 0) })
	})
	t.Run("postgres", func(t *testing.T) {
		t.Run("commit persists", func(t *testing.T) { assertRootTeardown(t, postgresDialect(), database.Commit, 1) })
		t.Run("rollback discards", func(t *testing.T) { assertRootTeardown(t, postgresDialect(), database.Rollback, 0) })
	})
}

// assertRootTeardown writes a row in a root transaction, ends it with
// teardown, and checks the row count another session then sees.
func assertRootTeardown(t *testing.T, d dialect, teardown func(context.Context) error, want int) {
	t.Helper()
	ctx := context.Background()
	cleanTables(t, d.db)
	txCtx, err := database.NewTransaction(ctx, stdlibadapter.New(d.db), "root")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	if _, err := database.FromContext(txCtx).Exec(txCtx, d.insert, "root", "root@example.com"); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if err := teardown(txCtx); err != nil {
		t.Fatalf("teardown error = %v", err)
	}
	if !database.FromContext(txCtx).IsClosed() {
		t.Error("IsClosed() = false after the root teardown")
	}
	if got := d.rows(t, "root@example.com"); got != want {
		t.Errorf("rows after the teardown = %d, want %d", got, want)
	}
}

func TestNestedTransactionSavepoints(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) {
		t.Run("inner rollback keeps outer writes", func(t *testing.T) { assertInnerRollbackKeepsOuter(t, mysqlDialect()) })
		t.Run("inner release keeps inner writes", func(t *testing.T) { assertInnerReleaseKeepsInner(t, mysqlDialect()) })
	})
	t.Run("sqlite", func(t *testing.T) {
		t.Run("inner rollback keeps outer writes", func(t *testing.T) { assertInnerRollbackKeepsOuter(t, sqliteDialect()) })
		t.Run("inner release keeps inner writes", func(t *testing.T) { assertInnerReleaseKeepsInner(t, sqliteDialect()) })
	})
	t.Run("postgres", func(t *testing.T) {
		t.Run("inner rollback keeps outer writes", func(t *testing.T) { assertInnerRollbackKeepsOuter(t, postgresDialect()) })
		t.Run("inner release keeps inner writes", func(t *testing.T) { assertInnerReleaseKeepsInner(t, postgresDialect()) })
	})
}

// assertInnerRollbackKeepsOuter fails a nested WithTransaction after it wrote,
// then writes again in the outer scope and commits: the savepoint's write is
// gone, and the outer writes on either side of it persist.
func assertInnerRollbackKeepsOuter(t *testing.T, d dialect) {
	t.Helper()
	ctx := context.Background()
	cleanTables(t, d.db)
	q := stdlibadapter.New(d.db)
	errInner := errors.New("inner scope failed")

	err := database.WithTransaction(ctx, q, "outer_scope", func(txCtx context.Context) error {
		if _, err := database.Conn(txCtx, q).Exec(txCtx, d.insert, "outer", "outer@example.com"); err != nil {
			return err
		}
		innerErr := database.WithTransaction(txCtx, q, "inner_scope", func(spCtx context.Context) error {
			if _, err := database.Conn(spCtx, q).Exec(spCtx, d.insert, "inner", "inner@example.com"); err != nil {
				return err
			}
			return errInner
		})
		if !errors.Is(innerErr, errInner) {
			t.Errorf("inner WithTransaction() error = %v, want %v", innerErr, errInner)
		}
		_, err := database.Conn(txCtx, q).Exec(txCtx, d.insert, "after", "after@example.com")
		return err
	})
	if err != nil {
		t.Fatalf("outer WithTransaction() error = %v", err)
	}

	for _, c := range []struct {
		email string
		want  int
	}{{"outer@example.com", 1}, {"inner@example.com", 0}, {"after@example.com", 1}} {
		if got := d.rows(t, c.email); got != c.want {
			t.Errorf("rows with email %q = %d, want %d", c.email, got, c.want)
		}
	}
}

// assertInnerReleaseKeepsInner lets a nested WithTransaction succeed and
// commits the outer one: the savepoint's write persists with the outer one.
func assertInnerReleaseKeepsInner(t *testing.T, d dialect) {
	t.Helper()
	ctx := context.Background()
	cleanTables(t, d.db)
	q := stdlibadapter.New(d.db)

	err := database.WithTransaction(ctx, q, "outer_scope", func(txCtx context.Context) error {
		if _, err := database.Conn(txCtx, q).Exec(txCtx, d.insert, "outer", "outer@example.com"); err != nil {
			return err
		}
		return database.WithTransaction(txCtx, q, "inner_scope", func(spCtx context.Context) error {
			_, err := database.Conn(spCtx, q).Exec(spCtx, d.insert, "inner", "inner@example.com")
			return err
		})
	})
	if err != nil {
		t.Fatalf("outer WithTransaction() error = %v", err)
	}

	for _, c := range []struct {
		email string
		want  int
	}{{"outer@example.com", 1}, {"inner@example.com", 1}} {
		if got := d.rows(t, c.email); got != c.want {
			t.Errorf("rows with email %q = %d, want %d", c.email, got, c.want)
		}
	}
}

// TestReservationSerializesTwoGoroutinesOverAnOpenResultSet holds a result set
// open on one goroutine while a second goroutine issues a statement on the
// same txCtx. The second must wait for the result set to be released and then
// succeed. Without the wait MySQL rejects it (`busy buffer`), while SQLite
// runs it at once; either way it returns while the result set is still open.
func TestReservationSerializesTwoGoroutinesOverAnOpenResultSet(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) { assertSecondGoroutineWaits(t, mysqlDialect()) })
	t.Run("sqlite", func(t *testing.T) { assertSecondGoroutineWaits(t, sqliteDialect()) })
	t.Run("postgres", func(t *testing.T) { assertSecondGoroutineWaits(t, postgresDialect()) })
}

func assertSecondGoroutineWaits(t *testing.T, d dialect) {
	t.Helper()
	ctx := context.Background()
	cleanTables(t, d.db)
	q := stdlibadapter.New(d.db)
	for _, email := range []string{"r1@example.com", "r2@example.com", "r3@example.com"} {
		if _, err := q.Exec(ctx, d.insert, "seed", email); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	txCtx, err := database.NewTransaction(ctx, q, "two_goroutines")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)
	defer func() { _ = database.Rollback(txCtx) }()

	rows, err := tx.Query(txCtx, "SELECT id FROM test_users ORDER BY id")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if !rows.Next() {
		t.Fatalf("Next() = false on the first row, want true (err %v)", rows.Err())
	}

	done := make(chan error, 1)
	go func() {
		_, err := tx.Exec(txCtx, d.insert, "second", "second@example.com")
		done <- err
	}()

	select {
	case err := <-done:
		_ = rows.Close()
		t.Fatalf("Exec() from a second goroutine returned (error %v) while the first held an open result set, want it to wait", err)
	case <-time.After(holdWindow):
	}

	n := 1
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Errorf("rows.Err() = %v", err)
	}
	if n != 3 {
		t.Errorf("read %d rows, want 3", n)
	}
	if err := rows.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if err := <-done; err != nil {
		t.Errorf("Exec() from the second goroutine after the release = %v, want nil", err)
	}
	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() = %v, want nil", err)
	}
	if got := d.rows(t, "second@example.com"); got != 1 {
		t.Errorf("rows written by the second goroutine = %d, want 1", got)
	}
}

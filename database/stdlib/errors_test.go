package stdlib

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	"github.com/lib/pq/pqerror"

	"github.com/teandresmith/sqlgen/database"
)

// --- lib/pq tests ---

func TestMapPqErrorConstraintCodes(t *testing.T) {
	tests := []struct {
		name           string
		code           string
		constraintName string
		columnName     string
		detail         string
		wantType       database.ConstraintType
	}{
		{
			name:           "23505 unique violation",
			code:           "23505",
			constraintName: "users_email_key",
			columnName:     "email",
			detail:         "Key (email)=(test@test.com) already exists.",
			wantType:       database.ConstraintUnique,
		},
		{
			name:           "23503 foreign key violation",
			code:           "23503",
			constraintName: "orders_user_id_fkey",
			columnName:     "user_id",
			detail:         "Key (user_id)=(999) is not present in table \"users\".",
			wantType:       database.ConstraintForeignKey,
		},
		{
			name:           "23502 not null violation",
			code:           "23502",
			constraintName: "users_name_not_null",
			columnName:     "name",
			detail:         "Failing row contains (1, null, test@test.com).",
			wantType:       database.ConstraintNotNull,
		},
		{
			name:           "23514 check violation",
			code:           "23514",
			constraintName: "users_age_check",
			columnName:     "age",
			detail:         "Failing row contains (1, alice, -1).",
			wantType:       database.ConstraintCheck,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pqErr := &pq.Error{
				Code:       pqerror.Code(tt.code),
				Constraint: tt.constraintName,
				Column:     tt.columnName,
				Detail:     tt.detail,
			}

			ok, got := mapPqError(pqErr)
			if !ok {
				t.Fatalf("mapPqError(%s) returned ok=false, want true", tt.code)
			}

			var ce *database.ConstraintError
			if !errors.As(got, &ce) {
				t.Fatalf("mapPqError(%s) did not return ConstraintError, got %T: %v", tt.code, got, got)
			}
			if ce.Type != tt.wantType {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, tt.wantType)
			}
			if ce.Constraint != tt.constraintName {
				t.Errorf("ConstraintError.Constraint = %q, want %q", ce.Constraint, tt.constraintName)
			}
			if ce.Column != tt.columnName {
				t.Errorf("ConstraintError.Column = %q, want %q", ce.Column, tt.columnName)
			}
			if ce.Detail != tt.detail {
				t.Errorf("ConstraintError.Detail = %q, want %q", ce.Detail, tt.detail)
			}
			if !errors.Is(got, database.ErrConstraintViolation) {
				t.Error("errors.Is(mapped, ErrConstraintViolation) = false, want true")
			}
		})
	}
}

func TestMapPqErrorDeadlock(t *testing.T) {
	pqErr := &pq.Error{Code: "40P01"}

	ok, got := mapPqError(pqErr)

	if !ok {
		t.Fatal("mapPqError(40P01) returned ok=false, want true")
	}
	if !errors.Is(got, database.ErrDeadlock) {
		t.Errorf("mapPqError(40P01) is not ErrDeadlock: %v", got)
	}
}

func TestMapPqErrorConnectionFailed(t *testing.T) {
	codes := []string{"08000", "08003", "08006"}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			pqErr := &pq.Error{Code: pqerror.Code(code)}

			ok, got := mapPqError(pqErr)

			if !ok {
				t.Fatalf("mapPqError(%s) returned ok=false, want true", code)
			}
			if !errors.Is(got, database.ErrConnectionFailed) {
				t.Errorf("mapPqError(%s) is not ErrConnectionFailed: %v", code, got)
			}
		})
	}
}

func TestMapPqErrorUnrecognized(t *testing.T) {
	pqErr := &pq.Error{Code: "42P01"} // undefined_table

	ok, _ := mapPqError(pqErr)

	if ok {
		t.Error("mapPqError(42P01) returned ok=true, want false for unrecognized code")
	}
}

// --- MySQL tests ---

func TestMapMySQLErrorConstraintCodes(t *testing.T) {
	tests := []struct {
		name     string
		number   uint16
		message  string
		wantType database.ConstraintType
	}{
		{
			name:     "1062 unique violation",
			number:   1062,
			message:  "Duplicate entry 'Widget' for key 'products.name'",
			wantType: database.ConstraintUnique,
		},
		{
			name:     "1451 FK violation parent",
			number:   1451,
			message:  "Cannot delete or update a parent row: a foreign key constraint fails (`db`.`orders`, CONSTRAINT `orders_user_id_fk` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`))",
			wantType: database.ConstraintForeignKey,
		},
		{
			name:     "1452 FK violation child",
			number:   1452,
			message:  "Cannot add or update a child row: a foreign key constraint fails (`db`.`orders`, CONSTRAINT `orders_user_id_fk` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`))",
			wantType: database.ConstraintForeignKey,
		},
		{
			name:     "1048 not null violation",
			number:   1048,
			message:  "Column 'name' cannot be null",
			wantType: database.ConstraintNotNull,
		},
		{
			name:     "3819 check violation",
			number:   3819,
			message:  "Check constraint 'users_age_check' is violated.",
			wantType: database.ConstraintCheck,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mysqlErr := &mysql.MySQLError{
				Number:  tt.number,
				Message: tt.message,
			}

			ok, got := mapMySQLError(mysqlErr)
			if !ok {
				t.Fatalf("mapMySQLError(%d) returned ok=false, want true", tt.number)
			}

			var ce *database.ConstraintError
			if !errors.As(got, &ce) {
				t.Fatalf("mapMySQLError(%d) did not return ConstraintError, got %T: %v", tt.number, got, got)
			}
			if ce.Type != tt.wantType {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, tt.wantType)
			}
			if ce.Detail != tt.message {
				t.Errorf("ConstraintError.Detail = %q, want %q", ce.Detail, tt.message)
			}
			if !errors.Is(got, database.ErrConstraintViolation) {
				t.Error("errors.Is(mapped, ErrConstraintViolation) = false, want true")
			}
		})
	}
}

func TestMapMySQLErrorDeadlock(t *testing.T) {
	mysqlErr := &mysql.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock"}

	ok, got := mapMySQLError(mysqlErr)

	if !ok {
		t.Fatal("mapMySQLError(1213) returned ok=false, want true")
	}
	if !errors.Is(got, database.ErrDeadlock) {
		t.Errorf("mapMySQLError(1213) is not ErrDeadlock: %v", got)
	}
}

func TestMapMySQLErrorConnectionFailed(t *testing.T) {
	numbers := []uint16{2002, 2006, 1040}
	for _, num := range numbers {
		t.Run(fmt.Sprintf("%d", num), func(t *testing.T) {
			mysqlErr := &mysql.MySQLError{Number: num, Message: "connection error"}

			ok, got := mapMySQLError(mysqlErr)

			if !ok {
				t.Fatalf("mapMySQLError(%d) returned ok=false, want true", num)
			}
			if !errors.Is(got, database.ErrConnectionFailed) {
				t.Errorf("mapMySQLError(%d) is not ErrConnectionFailed: %v", num, got)
			}
		})
	}
}

func TestMapMySQLErrorUnrecognized(t *testing.T) {
	mysqlErr := &mysql.MySQLError{Number: 1146, Message: "Table doesn't exist"}

	ok, _ := mapMySQLError(mysqlErr)

	if ok {
		t.Error("mapMySQLError(1146) returned ok=true, want false for unrecognized code")
	}
}

// --- MySQL parser tests ---

func TestParseMySQLConstraint(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "duplicate entry with table prefix",
			msg:  "Duplicate entry 'Widget' for key 'products.name'",
			want: "name",
		},
		{
			name: "duplicate entry without table prefix",
			msg:  "Duplicate entry 'Widget' for key 'name'",
			want: "name",
		},
		{
			name: "FK constraint",
			msg:  "Cannot delete or update a parent row: a foreign key constraint fails (`db`.`orders`, CONSTRAINT `orders_user_id_fk` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`))",
			want: "orders_user_id_fk",
		},
		{
			name: "check constraint",
			msg:  "Check constraint 'users_age_check' is violated.",
			want: "users_age_check",
		},
		{
			name: "unrecognized format",
			msg:  "something completely different",
			want: "",
		},
		{
			name: "empty message",
			msg:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMySQLConstraint(tt.msg)
			if got != tt.want {
				t.Errorf("parseMySQLConstraint(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestParseMySQLColumn(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "not null column name",
			msg:  "Column 'name' cannot be null",
			want: "name",
		},
		{
			name: "not null email column",
			msg:  "Column 'email' cannot be null",
			want: "email",
		},
		{
			name: "unrecognized format",
			msg:  "something completely different",
			want: "",
		},
		{
			name: "empty message",
			msg:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMySQLColumn(tt.msg)
			if got != tt.want {
				t.Errorf("parseMySQLColumn(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

// --- SQLite code mapping tests ---

func TestMapSQLiteCode(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		msg      string
		wantType database.ConstraintType
	}{
		{
			name:     "2067 SQLITE_CONSTRAINT_UNIQUE",
			code:     2067,
			msg:      "UNIQUE constraint failed: users.email",
			wantType: database.ConstraintUnique,
		},
		{
			name:     "1555 SQLITE_CONSTRAINT_PRIMARYKEY",
			code:     1555,
			msg:      "UNIQUE constraint failed: users.id",
			wantType: database.ConstraintUnique,
		},
		{
			name:     "787 SQLITE_CONSTRAINT_FOREIGNKEY",
			code:     787,
			msg:      "FOREIGN KEY constraint failed",
			wantType: database.ConstraintForeignKey,
		},
		{
			name:     "1299 SQLITE_CONSTRAINT_NOTNULL",
			code:     1299,
			msg:      "NOT NULL constraint failed: users.name",
			wantType: database.ConstraintNotNull,
		},
		{
			name:     "275 SQLITE_CONSTRAINT_CHECK",
			code:     275,
			msg:      "CHECK constraint failed: users_age_check",
			wantType: database.ConstraintCheck,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := errors.New("sqlite driver error")

			ok, got := mapSQLiteCode(tt.code, tt.msg, original)
			if !ok {
				t.Fatalf("mapSQLiteCode(%d) returned ok=false, want true", tt.code)
			}

			var ce *database.ConstraintError
			if !errors.As(got, &ce) {
				t.Fatalf("mapSQLiteCode(%d) did not return ConstraintError, got %T: %v", tt.code, got, got)
			}
			if ce.Type != tt.wantType {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, tt.wantType)
			}
			if ce.Detail != tt.msg {
				t.Errorf("ConstraintError.Detail = %q, want %q", ce.Detail, tt.msg)
			}
			if !errors.Is(got, database.ErrConstraintViolation) {
				t.Error("errors.Is(mapped, ErrConstraintViolation) = false, want true")
			}
		})
	}
}

func TestMapSQLiteCodeConnectionErrors(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{name: "SQLITE_BUSY", code: 5},
		{name: "SQLITE_CANTOPEN", code: 14},
		{name: "SQLITE_BUSY_RECOVERY", code: 261},      // extended code with primary 5
		{name: "SQLITE_CANTOPEN_NOTEMPDIR", code: 270}, // extended code with primary 14
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := errors.New("sqlite connection error")

			ok, got := mapSQLiteCode(tt.code, "database is locked", original)
			if !ok {
				t.Fatalf("mapSQLiteCode(%d) returned ok=false, want true", tt.code)
			}

			if !errors.Is(got, database.ErrConnectionFailed) {
				t.Errorf("mapSQLiteCode(%d) is not ErrConnectionFailed: %v", tt.code, got)
			}
		})
	}
}

func TestMapSQLiteCodeUnrecognized(t *testing.T) {
	original := errors.New("sqlite error")

	ok, _ := mapSQLiteCode(999, "unknown error", original)

	if ok {
		t.Error("mapSQLiteCode(999) returned ok=true, want false for unrecognized code")
	}
}

// --- SQLite parser tests ---

func TestParseSQLiteConstraint(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "unique constraint with table.column",
			msg:  "UNIQUE constraint failed: users.email",
			want: "users.email",
		},
		{
			name: "not null constraint",
			msg:  "NOT NULL constraint failed: users.name",
			want: "users.name",
		},
		{
			name: "check constraint name",
			msg:  "CHECK constraint failed: users_age_check",
			want: "users_age_check",
		},
		{
			name: "foreign key no detail",
			msg:  "FOREIGN KEY constraint failed",
			want: "",
		},
		{
			name: "modernc prefix unique",
			msg:  "constraint failed: UNIQUE constraint failed: slugged_rows.slug",
			want: "slugged_rows.slug",
		},
		{
			name: "modernc prefix check",
			msg:  "constraint failed: CHECK constraint failed: named_chk",
			want: "named_chk",
		},
		{
			name: "modernc prefix foreign key no detail",
			msg:  "constraint failed: FOREIGN KEY constraint failed",
			want: "",
		},
		{
			name: "unrecognized format",
			msg:  "something else entirely",
			want: "",
		},
		{
			name: "empty message",
			msg:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSQLiteConstraint(tt.msg)
			if got != tt.want {
				t.Errorf("parseSQLiteConstraint(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestParseSQLiteColumn(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "unique constraint extracts column",
			msg:  "UNIQUE constraint failed: users.email",
			want: "email",
		},
		{
			name: "not null constraint extracts column",
			msg:  "NOT NULL constraint failed: users.name",
			want: "name",
		},
		{
			name: "check constraint name only (no dot)",
			msg:  "CHECK constraint failed: users_age_check",
			want: "",
		},
		{
			name: "modernc prefix unique extracts column",
			msg:  "constraint failed: UNIQUE constraint failed: slugged_rows.slug",
			want: "slug",
		},
		{
			name: "foreign key no detail",
			msg:  "FOREIGN KEY constraint failed",
			want: "",
		},
		{
			name: "empty message",
			msg:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSQLiteColumn(tt.msg)
			if got != tt.want {
				t.Errorf("parseSQLiteColumn(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

// --- modernc SQLite interface-based detection test ---

// mockSQLiteError implements the sqliteError interface for testing.
type mockSQLiteError struct {
	code int
	msg  string
}

func (e *mockSQLiteError) Error() string { return e.msg }
func (e *mockSQLiteError) Code() int     { return e.code }

func TestMapModerncSQLiteError(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		msg      string
		wantType database.ConstraintType
	}{
		{
			name:     "unique violation via interface",
			code:     2067,
			msg:      "UNIQUE constraint failed: users.email",
			wantType: database.ConstraintUnique,
		},
		{
			name:     "FK violation via interface",
			code:     787,
			msg:      "FOREIGN KEY constraint failed",
			wantType: database.ConstraintForeignKey,
		},
		{
			name:     "not null violation via interface",
			code:     1299,
			msg:      "NOT NULL constraint failed: users.name",
			wantType: database.ConstraintNotNull,
		},
		{
			name:     "check violation via interface",
			code:     275,
			msg:      "CHECK constraint failed: users_age_check",
			wantType: database.ConstraintCheck,
		},
		{
			name:     "primary key unique via interface",
			code:     1555,
			msg:      "UNIQUE constraint failed: users.id",
			wantType: database.ConstraintUnique,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockErr := &mockSQLiteError{code: tt.code, msg: tt.msg}

			ok, got := mapModerncSQLiteError(mockErr)
			if !ok {
				t.Fatalf("mapModerncSQLiteError(%d) returned ok=false, want true", tt.code)
			}

			var ce *database.ConstraintError
			if !errors.As(got, &ce) {
				t.Fatalf("mapModerncSQLiteError(%d) did not return ConstraintError, got %T: %v", tt.code, got, got)
			}
			if ce.Type != tt.wantType {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, tt.wantType)
			}
			if !errors.Is(got, database.ErrConstraintViolation) {
				t.Error("errors.Is(mapped, ErrConstraintViolation) = false, want true")
			}
		})
	}
}

// TestMapModerncSQLiteErrorParsesDriverMessage feeds the messages
// modernc.org/sqlite actually produces ("<result code text>: <sqlite message>
// (<extended code>)", as measured against the driver) through the mapper, so
// neither the prefix nor the code suffix leaks into Constraint or Column.
func TestMapModerncSQLiteErrorParsesDriverMessage(t *testing.T) {
	tests := []struct {
		name           string
		code           int
		msg            string
		wantConstraint string
		wantColumn     string
	}{
		{
			name:           "unique",
			code:           2067,
			msg:            "constraint failed: UNIQUE constraint failed: slugged_rows.slug (2067)",
			wantConstraint: "slugged_rows.slug",
			wantColumn:     "slug",
		},
		{
			name:           "primary key",
			code:           1555,
			msg:            "constraint failed: UNIQUE constraint failed: u.id (1555)",
			wantConstraint: "u.id",
			wantColumn:     "id",
		},
		{
			name:           "not null",
			code:           1299,
			msg:            "constraint failed: NOT NULL constraint failed: u.name (1299)",
			wantConstraint: "u.name",
			wantColumn:     "name",
		},
		{
			name:           "named check",
			code:           275,
			msg:            "constraint failed: CHECK constraint failed: named_chk (275)",
			wantConstraint: "named_chk",
			wantColumn:     "",
		},
		{
			name:           "foreign key carries no identifier",
			code:           787,
			msg:            "constraint failed: FOREIGN KEY constraint failed (787)",
			wantConstraint: "",
			wantColumn:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := mapModerncSQLiteError(&mockSQLiteError{code: tt.code, msg: tt.msg})

			ce, ok := errors.AsType[*database.ConstraintError](got)
			if !ok {
				t.Fatalf("mapModerncSQLiteError(%d) = %T (%v), want a *database.ConstraintError", tt.code, got, got)
			}
			if ce.Constraint != tt.wantConstraint {
				t.Errorf("ConstraintError.Constraint = %q, want %q", ce.Constraint, tt.wantConstraint)
			}
			if ce.Column != tt.wantColumn {
				t.Errorf("ConstraintError.Column = %q, want %q", ce.Column, tt.wantColumn)
			}
			if ce.Detail != tt.msg {
				t.Errorf("ConstraintError.Detail = %q, want the driver message %q", ce.Detail, tt.msg)
			}
		})
	}
}

func TestMapModerncSQLiteErrorUnrecognized(t *testing.T) {
	mockErr := &mockSQLiteError{code: 999, msg: "unknown"}

	ok, _ := mapModerncSQLiteError(mockErr)

	if ok {
		t.Error("mapModerncSQLiteError(999) returned ok=true, want false for unrecognized code")
	}
}

func TestMapModerncSQLiteErrorNonSQLite(t *testing.T) {
	original := errors.New("not a sqlite error")

	ok, _ := mapModerncSQLiteError(original)

	if ok {
		t.Error("mapModerncSQLiteError(non-sqlite) returned ok=true, want false")
	}
}

// --- Top-level MapError dispatch test ---

func TestMapErrorDispatchesPq(t *testing.T) {
	pqErr := &pq.Error{
		Code:       "23505",
		Constraint: "test_key",
		Column:     "col",
		Detail:     "duplicate",
	}

	ok, got := MapError(pqErr)
	if !ok {
		t.Fatal("MapError(pq 23505) returned ok=false, want true")
	}

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatalf("MapError(pq 23505) did not return ConstraintError")
	}
	if ce.Type != database.ConstraintUnique {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintUnique)
	}
}

func TestMapErrorDispatchesMySQL(t *testing.T) {
	mysqlErr := &mysql.MySQLError{
		Number:  1062,
		Message: "Duplicate entry 'test' for key 'test.name'",
	}

	ok, got := MapError(mysqlErr)
	if !ok {
		t.Fatal("MapError(mysql 1062) returned ok=false, want true")
	}

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatalf("MapError(mysql 1062) did not return ConstraintError")
	}
	if ce.Type != database.ConstraintUnique {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintUnique)
	}
}

func TestMapErrorDispatchesSQLite(t *testing.T) {
	mockErr := &mockSQLiteError{code: 2067, msg: "UNIQUE constraint failed: t.c"}

	ok, got := MapError(mockErr)
	if !ok {
		t.Fatal("MapError(sqlite 2067) returned ok=false, want true")
	}

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatalf("MapError(sqlite 2067) did not return ConstraintError")
	}
	if ce.Type != database.ConstraintUnique {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintUnique)
	}
}

func TestMapErrorNil(t *testing.T) {
	ok, got := MapError(nil)
	if ok || got != nil {
		t.Errorf("MapError(nil) = (%v, %v), want (false, nil)", ok, got)
	}
}

func TestMapErrorUnrecognized(t *testing.T) {
	ok, _ := MapError(errors.New("unknown driver error"))

	if ok {
		t.Error("MapError(unknown) returned ok=true, want false")
	}
}

// --- SQLite fallback tests ---

func TestMapSQLiteCodeConstraintFieldsPopulated(t *testing.T) {
	original := errors.New("driver error")

	_, got := mapSQLiteCode(2067, "UNIQUE constraint failed: users.email", original)

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatal("expected ConstraintError")
	}
	if ce.Constraint != "users.email" {
		t.Errorf("ConstraintError.Constraint = %q, want %q", ce.Constraint, "users.email")
	}
	if ce.Column != "email" {
		t.Errorf("ConstraintError.Column = %q, want %q", ce.Column, "email")
	}
	if ce.Detail != "UNIQUE constraint failed: users.email" {
		t.Errorf("ConstraintError.Detail = %q, want message", ce.Detail)
	}
}

func TestMapSQLiteCodeFKNoDetail(t *testing.T) {
	original := errors.New("driver error")

	_, got := mapSQLiteCode(787, "FOREIGN KEY constraint failed", original)

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatal("expected ConstraintError")
	}
	// FK messages in SQLite don't include table.column detail
	if ce.Constraint != "" {
		t.Errorf("ConstraintError.Constraint = %q, want empty (FK has no detail)", ce.Constraint)
	}
	if ce.Column != "" {
		t.Errorf("ConstraintError.Column = %q, want empty", ce.Column)
	}
}

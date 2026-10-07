package cli

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

func TestConnString(t *testing.T) {
	const awkward = "p@ss:w/rd%?#"
	tests := []struct {
		name          string
		dialect       config.Dialect
		conn          *config.ConnectionConfig
		want          string
		wantErrFields []string
	}{
		{
			name:    "postgres url wins over the fields",
			dialect: config.DialectPostgres,
			conn:    &config.ConnectionConfig{URL: "postgres://u@h/d?sslmode=verify-full", Host: "other", User: "x", Database: "y"},
			want:    "postgres://u@h/d?sslmode=verify-full",
		},
		{
			name:    "mysql url wins over the fields",
			dialect: config.DialectMySQL,
			conn:    &config.ConnectionConfig{URL: "u:p@tcp(h:1)/d", Host: "other", User: "x", Database: "y"},
			want:    "u:p@tcp(h:1)/d",
		},
		{
			name:    "sqlite url wins over database",
			dialect: config.DialectSQLite,
			conn:    &config.ConnectionConfig{URL: "file:app.db?mode=ro", Database: "other.db"},
			want:    "file:app.db?mode=ro",
		},
		{ //nolint:gosec // test fixture, not a real credential
			name:    "postgres every field set",
			dialect: config.DialectPostgres,
			conn:    &config.ConnectionConfig{Host: "db.example", Port: 6543, User: "app", Password: "secret", Database: "appdb", SSLMode: "require"},
			want:    "postgres://app:secret@db.example:6543/appdb?sslmode=require",
		},
		{
			name:    "postgres port, ssl_mode and empty password default",
			dialect: config.DialectPostgres,
			conn:    &config.ConnectionConfig{Host: "localhost", User: "app", Database: "appdb"},
			want:    "postgres://app@localhost:5432/appdb?sslmode=disable",
		},
		{ //nolint:gosec // test fixture, not a real credential
			name:    "postgres password with reserved characters is escaped",
			dialect: config.DialectPostgres,
			conn:    &config.ConnectionConfig{Host: "localhost", User: "app", Password: awkward, Database: "appdb"},
			want:    "postgres://app:p%40ss%3Aw%2Frd%25%3F%23@localhost:5432/appdb?sslmode=disable",
		},
		{
			name:    "postgres IPv6 host is bracketed",
			dialect: config.DialectPostgres,
			conn:    &config.ConnectionConfig{Host: "::1", User: "app", Database: "appdb"},
			want:    "postgres://app@[::1]:5432/appdb?sslmode=disable",
		},
		{
			name:    "postgres socket directory goes in the host parameter",
			dialect: config.DialectPostgres,
			conn:    &config.ConnectionConfig{Host: "/var/run/postgresql", User: "app", Database: "appdb"},
			want:    "postgres://app@/appdb?host=%2Fvar%2Frun%2Fpostgresql&port=5432&sslmode=disable",
		},
		{ //nolint:gosec // test fixture, not a real credential
			name:    "postgres socket directory with port and password",
			dialect: config.DialectPostgres,
			conn:    &config.ConnectionConfig{Host: "/tmp", Port: 6543, User: "app", Password: "secret", Database: "appdb", SSLMode: "require"},
			want:    "postgres://app:secret@/appdb?host=%2Ftmp&port=6543&sslmode=require",
		},
		{
			name:    "mysql every field set",
			dialect: config.DialectMySQL,
			conn:    &config.ConnectionConfig{Host: "db.example", Port: 3307, User: "app", Password: "secret", Database: "appdb", SSLMode: "skip-verify"},
			want:    "app:secret@tcp(db.example:3307)/appdb?tls=skip-verify",
		},
		{
			name:    "mysql port, ssl_mode and empty password default",
			dialect: config.DialectMySQL,
			conn:    &config.ConnectionConfig{Host: "localhost", User: "app", Database: "appdb"},
			want:    "app@tcp(localhost:3306)/appdb?tls=false",
		},
		{
			name:    "mysql socket file goes in unix() and the port is dropped",
			dialect: config.DialectMySQL,
			conn:    &config.ConnectionConfig{Host: "/var/run/mysqld/mysqld.sock", Port: 3307, User: "app", Password: awkward, Database: "appdb"},
			want:    "app:" + awkward + "@unix(/var/run/mysqld/mysqld.sock)/appdb?tls=false",
		},
		{
			name:    "mysql password with reserved characters goes in raw",
			dialect: config.DialectMySQL,
			conn:    &config.ConnectionConfig{Host: "localhost", User: "app", Password: awkward, Database: "appdb"},
			want:    "app:" + awkward + "@tcp(localhost:3306)/appdb?tls=false",
		},
		{
			name:    "sqlite uses the database path and ignores host fields",
			dialect: config.DialectSQLite,
			conn:    &config.ConnectionConfig{Host: "ignored", Port: 1, Database: "./data/app.db"},
			want:    "./data/app.db",
		},
		{
			name:          "postgres with no fields names every required one",
			dialect:       config.DialectPostgres,
			conn:          &config.ConnectionConfig{Port: 5432},
			wantErrFields: []string{"host", "user", "database"},
		},
		{
			name:          "mysql missing user only",
			dialect:       config.DialectMySQL,
			conn:          &config.ConnectionConfig{Host: "localhost", Database: "appdb"},
			wantErrFields: []string{"user"},
		},
		{
			name:          "mysql missing host only",
			dialect:       config.DialectMySQL,
			conn:          &config.ConnectionConfig{User: "app", Database: "appdb"},
			wantErrFields: []string{"host"},
		},
		{
			name:          "sqlite missing database",
			dialect:       config.DialectSQLite,
			conn:          &config.ConnectionConfig{Host: "localhost"},
			wantErrFields: []string{"database"},
		},
		{
			name:          "mysql user containing a colon",
			dialect:       config.DialectMySQL,
			conn:          &config.ConnectionConfig{Host: "localhost", User: "ap:p", Password: "secret", Database: "appdb"},
			wantErrFields: []string{"user"},
		},
		{
			name:    "postgres user containing a colon is escaped",
			dialect: config.DialectPostgres,
			conn:    &config.ConnectionConfig{Host: "localhost", User: "ap:p", Database: "appdb"},
			want:    "postgres://ap%3Ap@localhost:5432/appdb?sslmode=disable",
		},
		{
			name:          "nil connection",
			dialect:       config.DialectSQLite,
			wantErrFields: []string{"database"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := connString(tt.dialect, tt.conn)
			if len(tt.wantErrFields) > 0 {
				if err == nil {
					t.Fatalf("connString(%s) = %q, want an error naming %v", tt.dialect, got, tt.wantErrFields)
				}
				lines := strings.Split(err.Error(), "\n")
				if len(lines) != len(tt.wantErrFields) {
					t.Fatalf("connString(%s) error = %q, want %d errors naming %v", tt.dialect, err, len(tt.wantErrFields), tt.wantErrFields)
				}
				for i, field := range tt.wantErrFields {
					if !strings.HasPrefix(lines[i], "input.connection."+field+" ") {
						t.Errorf("connString(%s) error %d = %q, want it to name input.connection.%s", tt.dialect, i, lines[i], field)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("connString(%s) unexpected error: %v", tt.dialect, err)
			}
			if got != tt.want {
				t.Errorf("connString(%s) = %q, want %q", tt.dialect, got, tt.want)
			}
		})
	}
}

// TestConnString_PostgresPasswordRoundTrips parses the built URL back and
// checks every field survives, for passwords holding each URL delimiter.
func TestConnString_PostgresPasswordRoundTrips(t *testing.T) {
	for _, pw := range []string{"a@b", "a:b", "a/b", "a%b", "a?b#c", "@:/%", "spa ce", "ünï"} {
		t.Run(pw, func(t *testing.T) {
			conn := &config.ConnectionConfig{Host: "localhost", User: "app", Password: pw, Database: "appdb"}
			s, err := connString(config.DialectPostgres, conn)
			if err != nil {
				t.Fatalf("connString() unexpected error: %v", err)
			}
			u, err := url.Parse(s)
			if err != nil {
				t.Fatalf("url.Parse(%q) unexpected error: %v", s, err)
			}
			if got, _ := u.User.Password(); got != pw {
				t.Errorf("url.Parse(%q) password = %q, want %q", s, got, pw)
			}
			if got := u.User.Username(); got != "app" {
				t.Errorf("url.Parse(%q) user = %q, want %q", s, got, "app")
			}
			if u.Host != "localhost:5432" || u.Path != "/appdb" {
				t.Errorf("url.Parse(%q) host, path = %q, %q, want %q, %q", s, u.Host, u.Path, "localhost:5432", "/appdb")
			}
		})
	}
}

func TestConnString_PostgresSocketRoundTrips(t *testing.T) {
	conn := &config.ConnectionConfig{Host: "/var/run/postgresql", Port: 6543, User: "app", Password: "p@ss:w/rd", Database: "appdb"}
	s, err := connString(config.DialectPostgres, conn)
	if err != nil {
		t.Fatalf("connString() unexpected error: %v", err)
	}
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse(%q) unexpected error: %v", s, err)
	}
	q := u.Query()
	if got := q.Get("host"); got != conn.Host {
		t.Errorf("url.Parse(%q) host parameter = %q, want %q", s, got, conn.Host)
	}
	if got := q.Get("port"); got != "6543" {
		t.Errorf("url.Parse(%q) port parameter = %q, want %q", s, got, "6543")
	}
	if got, _ := u.User.Password(); got != conn.Password {
		t.Errorf("url.Parse(%q) password = %q, want %q", s, got, conn.Password)
	}
	if u.Host != "" || u.Path != "/appdb" {
		t.Errorf("url.Parse(%q) host, path = %q, %q, want %q, %q", s, u.Host, u.Path, "", "/appdb")
	}
}

// TestValidate_ConnectionFieldsMissing runs `sqlgen validate` on a database
// source with an incomplete connection: exit 2, every missing field named.
func TestValidate_ConnectionFieldsMissing(t *testing.T) {
	t.Setenv("SQLGEN_DB_URL", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	content := "input:\n  dialect: mysql\n  source: database\n  connection:\n    port: 3306\n" +
		"output:\n  driver: stdlib\n  dir: " + filepath.Join(dir, "output") + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	_, stderr, err := executeCommand("validate", "--config", cfgPath)
	if ee, ok := errors.AsType[*exitError](err); !ok || ee.code != ExitConfig {
		t.Fatalf("validate error = %v, want exit code %d\nstderr: %s", err, ExitConfig, stderr)
	}
	// Each once: validate does not try the connection pre-parse rejected.
	for _, field := range []string{"host", "user", "database"} {
		if n := strings.Count(stderr, "input.connection."+field+" is required"); n != 1 {
			t.Errorf("validate stderr = %q, want it to name input.connection.%s once, got %d", stderr, field, n)
		}
	}
}

func TestValidate_UnsetEnvReference(t *testing.T) {
	t.Setenv("SQLGEN_TEST_DB_HOST", "")
	if err := os.Unsetenv("SQLGEN_TEST_DB_HOST"); err != nil {
		t.Fatalf("unsetting SQLGEN_TEST_DB_HOST: %v", err)
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	content := "input:\n  dialect: postgres\n  source: database\n  connection:\n    host: ${SQLGEN_TEST_DB_HOST}\n" +
		"    user: app\n    database: app\noutput:\n  dir: " + filepath.Join(dir, "output") + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	_, stderr, err := executeCommand("validate", "--config", cfgPath)
	if ee, ok := errors.AsType[*exitError](err); !ok || ee.code != ExitConfig {
		t.Fatalf("validate error = %v, want exit code %d\nstderr: %s", err, ExitConfig, stderr)
	}
	want := "line 5: input.connection.host: environment variable SQLGEN_TEST_DB_HOST is not set"
	if !strings.Contains(stderr, want) {
		t.Errorf("validate stderr = %q, want it to contain %q", stderr, want)
	}
}

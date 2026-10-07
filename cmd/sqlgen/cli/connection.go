package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
)

// connectionError is a failure to reach the database or read its catalog.
// It wraps the driver's error, never the connection string, which can carry
// the password.
type connectionError struct {
	err error
}

func (e *connectionError) Error() string { return e.err.Error() }

func (e *connectionError) Unwrap() error { return e.err }

// introspectSchema reads the schema from the database that input.connection
// names (PRD §6.1). input.introspect narrows what is read; top-level
// exclude_tables applies later, as it does for every source.
func introspectSchema(ctx context.Context, cfg *config.RootConfig) (*parser.Schema, error) {
	conn, err := connString(cfg.Input.Dialect, cfg.Input.Connection)
	if err != nil {
		return nil, err
	}

	var intro introspect.Introspector
	switch cfg.Input.Dialect {
	case config.DialectPostgres:
		if err := postgresURLParses(conn); err != nil {
			return nil, &connectionError{err: err}
		}
		intro = introspect.NewPostgresIntrospector()
	case config.DialectMySQL:
		intro = introspect.NewMySQLIntrospector()
	case config.DialectSQLite:
		if err := sqliteFileExists(conn); err != nil {
			return nil, &connectionError{err: err}
		}
		intro = introspect.NewSQLiteIntrospector()
	default:
		return nil, fmt.Errorf("unsupported dialect: %q", cfg.Input.Dialect)
	}
	defer func() { _ = intro.Close() }()

	var opts introspect.IntrospectionOptions
	if in := cfg.Input.Introspect; in != nil {
		opts = introspect.IntrospectionOptions{
			Schemas:        in.Schemas,
			ExcludeSchemas: in.ExcludeSchemas,
			Tables:         in.Tables,
		}
	}

	schema := &parser.Schema{}
	if err := intro.Introspect(ctx, conn, schema, opts); err != nil {
		return nil, &connectionError{err: err}
	}
	return schema, nil
}

// postgresURLParses rejects a postgres URL that does not parse, without the
// parse error: it quotes the text around the fault, which is often the
// password (a literal '/', '#' or '%' in it breaks the URL, PRD §4.3).
func postgresURLParses(conn string) error {
	if !strings.HasPrefix(conn, "postgres://") && !strings.HasPrefix(conn, "postgresql://") {
		return nil
	}
	if _, err := url.Parse(conn); err != nil {
		return errors.New("introspect postgres: the connection URL does not parse; percent-encode reserved characters in the password")
	}
	return nil
}

// sqliteFileExists reports a missing SQLite database file. The driver would
// create an empty database in its place and introspect no tables.
func sqliteFileExists(conn string) error {
	path, ok := sqliteFilePath(conn)
	if !ok {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("introspect sqlite: database file %q does not exist", path)
	}
	return nil
}

// sqliteFilePath returns the file conn opens. The driver reads anything
// after '?' as parameters. A file: URI that sets mode, or that does not
// parse, is left to the driver: ok is false.
func sqliteFilePath(conn string) (path string, ok bool) {
	path, query, _ := strings.Cut(conn, "?")
	if !strings.HasPrefix(path, "file:") {
		return path, true
	}
	params, err := url.ParseQuery(query)
	if err != nil || params.Has("mode") {
		return "", false
	}
	u, err := url.Parse(path)
	if err != nil {
		return "", false
	}
	if u.Opaque == "" {
		return u.Path, true
	}
	path, err = url.PathUnescape(u.Opaque)
	return path, err == nil
}

// connString builds the driver connection string for dialect from c (PRD
// §4.3). A set `url` is returned verbatim; otherwise the string is built from
// the individual fields, with the port and ssl_mode defaulted per dialect.
func connString(dialect config.Dialect, c *config.ConnectionConfig) (string, error) {
	if errs := c.Errors(dialect); len(errs) > 0 {
		return "", errors.Join(errs...)
	}
	if c.URL != "" {
		return c.URL, nil
	}

	switch dialect {
	case config.DialectPostgres:
		port := portOrDefault(c.Port, 5432)
		query := url.Values{"sslmode": {valueOrDefault(c.SSLMode, "disable")}}
		u := url.URL{Scheme: "postgres", User: url.User(c.User), Path: "/" + c.Database}
		// A host starting with '/' is a Unix-socket directory, as in libpq. A
		// URL host cannot hold a path, so it goes in the host parameter.
		if strings.HasPrefix(c.Host, "/") {
			query.Set("host", c.Host)
			query.Set("port", port)
		} else {
			u.Host = net.JoinHostPort(c.Host, port)
		}
		u.RawQuery = query.Encode()
		if c.Password != "" {
			u.User = url.UserPassword(c.User, c.Password)
		}
		return u.String(), nil
	case config.DialectMySQL:
		// The go-sql-driver DSN is not a URL. Its parser splits on the last '/'
		// and then the last '@', so the password goes in raw; the database name
		// and parameters are unescaped.
		userinfo := c.User
		if c.Password != "" {
			userinfo += ":" + c.Password
		}
		// A host starting with '/' is the socket file itself, which has no port.
		addr := "tcp(" + net.JoinHostPort(c.Host, portOrDefault(c.Port, 3306)) + ")"
		if strings.HasPrefix(c.Host, "/") {
			addr = "unix(" + c.Host + ")"
		}
		return fmt.Sprintf("%s@%s/%s?tls=%s",
			userinfo, addr, url.PathEscape(c.Database),
			url.QueryEscape(valueOrDefault(c.SSLMode, "false"))), nil
	case config.DialectSQLite:
		return c.Database, nil
	default:
		return "", fmt.Errorf("input.dialect: unsupported dialect %q", dialect)
	}
}

func portOrDefault(port, def int) string {
	if port == 0 {
		port = def
	}
	return strconv.Itoa(port)
}

func valueOrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

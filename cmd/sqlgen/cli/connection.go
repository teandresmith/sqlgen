package cli

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

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

package gen

// BuildConnectionContext builds the ConnectionContext for the connection template.
func BuildConnectionContext(pkg string) ConnectionContext {
	return ConnectionContext{
		Package: pkg,
		Imports: []string{
			"database/sql/driver",
			"encoding/base64",
			"encoding/json",
			"github.com/teandresmith/sqlgen/sql",
		},
	}
}

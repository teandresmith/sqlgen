module github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_top_level

go 1.27.0

require (
	github.com/99designs/gqlgen v0.17.95
	github.com/jackc/pgx/v5 v5.9.1
	github.com/shopspring/decimal v1.4.0
	github.com/teandresmith/sqlgen v0.0.0
	github.com/vektah/gqlparser/v2 v2.5.37
	golang.org/x/sync v0.22.0
)

require (
	github.com/agnivade/levenshtein v1.2.1 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/sosodev/duration v1.4.0 // indirect
	github.com/urfave/cli/v3 v3.11.0 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
)

replace github.com/teandresmith/sqlgen => ../../../../..

tool github.com/99designs/gqlgen

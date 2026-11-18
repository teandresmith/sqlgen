module github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models

go 1.27.0

require (
	github.com/jackc/pgx/v5 v5.9.1
	github.com/teandresmith/sqlgen v0.0.0
	golang.org/x/sync v0.19.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	golang.org/x/text v0.34.0 // indirect
)

replace github.com/teandresmith/sqlgen => ../../../../../..

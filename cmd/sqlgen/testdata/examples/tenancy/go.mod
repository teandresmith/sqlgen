module github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy

go 1.27.0

require (
	github.com/google/go-cmp v0.7.0
	github.com/segmentio/ksuid v1.0.4
	github.com/teandresmith/sqlgen v0.0.0
	github.com/teandresmith/sqlgen/cache/memory v0.0.0-00010101000000-000000000000
	golang.org/x/sync v0.19.0
	modernc.org/sqlite v1.48.1
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-sql-driver/mysql v1.9.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/lib/pq v1.12.3 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/maypok86/otter/v2 v2.3.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/sys v0.42.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.70.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace github.com/teandresmith/sqlgen => ../../../../..

replace github.com/teandresmith/sqlgen/cache/memory => ../../../../../cache/memory

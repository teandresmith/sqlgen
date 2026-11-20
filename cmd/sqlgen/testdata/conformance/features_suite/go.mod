module github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features_suite

go 1.27.0

require (
	github.com/teandresmith/sqlgen v0.0.0
	github.com/teandresmith/sqlgen/cache/memory v0.0.0
	github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models v0.0.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/maypok86/otter/v2 v2.3.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/sync v0.19.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/teandresmith/sqlgen => ../../../../..
	github.com/teandresmith/sqlgen/cache/memory => ../../../../../cache/memory
	github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models => ../features_sqlite/models
)

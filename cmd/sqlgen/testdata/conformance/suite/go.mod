module github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/suite

go 1.27.0

require (
	github.com/google/go-cmp v0.7.0
	github.com/teandresmith/sqlgen v0.0.0
	github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models v0.0.0
)

require golang.org/x/sync v0.19.0 // indirect

replace (
	github.com/teandresmith/sqlgen => ../../../../..
	github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models => ../sqlite/models
)

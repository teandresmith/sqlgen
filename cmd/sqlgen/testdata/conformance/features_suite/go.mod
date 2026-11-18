module github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features_suite

go 1.27.0

require github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models v0.0.0

require github.com/teandresmith/sqlgen v0.0.0 // indirect

replace (
	github.com/teandresmith/sqlgen => ../../../../..
	github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models => ../features_sqlite/models
)

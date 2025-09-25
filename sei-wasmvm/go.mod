module github.com/CosmWasm/wasmvm

go 1.18

require (
	github.com/google/btree v1.0.0
	github.com/stretchr/testify v1.8.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

retract (
	// see https://github.com/CosmWasm/wasmvm/issues/459
	v1.4.0
	// originally published without the CWA-2023-004 fix
	v1.2.5
)

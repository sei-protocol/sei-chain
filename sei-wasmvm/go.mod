module github.com/CosmWasm/wasmvm

go 1.24.5

require (
	github.com/google/btree v1.1.3
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

retract (
	// see https://github.com/CosmWasm/wasmvm/issues/459
	v1.4.0
	// originally published without the CWA-2023-004 fix
	v1.2.5
)

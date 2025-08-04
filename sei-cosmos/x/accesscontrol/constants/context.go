package constants

type BadWasmDependencyAddressesKeyType string

const BadWasmDependencyAddressesKey = BadWasmDependencyAddressesKeyType("bad-wasm-dependency-addresses-key")

const ResetReasonBadWasmDependency = "incorrectly specified dependency access list"

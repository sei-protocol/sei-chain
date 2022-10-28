package constants

type BadWasmDependencyAddressesKeyType string

const BadWasmDependencyAddressesKey = BadWasmDependencyAddressesKeyType("key")

const ResetReasonBadWasmDependency = "incorrectly specified dependency access list"

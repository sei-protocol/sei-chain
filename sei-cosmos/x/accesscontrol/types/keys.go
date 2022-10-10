package types

import "encoding/binary"

// ResourceDependencyMappingKey is the key used for the keeper store
var (
	ResourceDependencyMappingKey = 0x01
	WasmMappingKey               = 0x02
)

const (
	// ModuleName defines the module name
	ModuleName = "accesscontrol"

	QuerierRoute = ModuleName

	// Append "acl" to prevent prefix collision with "acc" module
	StoreKey = "acl" + ModuleName

	RouterKey = ModuleName
)

func GetResourceDependencyMappingKey() []byte {
	return []byte{byte(ResourceDependencyMappingKey)}
}

func GetResourceDependencyKey(messageKey MessageKey) []byte {
	return append(GetResourceDependencyMappingKey(), []byte(messageKey)...)
}

func GetWasmMappingKey() []byte {
	return []byte{byte(WasmMappingKey)}
}

func GetKeyForCodeID(codeID uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, codeID)
	return key
}

func GetWasmCodeIDPrefix(codeID uint64) []byte {
	return append(GetWasmMappingKey(), GetKeyForCodeID(codeID)...)
}

// wasmFunctionName is the top level object key in the execute JSON payload
func GetWasmFunctionDependencyKey(codeID uint64, wasmFunctionName string) []byte {
	return append(GetWasmCodeIDPrefix(codeID), []byte(wasmFunctionName)...)
}

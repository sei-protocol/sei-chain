package types

// ResourceDependencyMappingKey is the key used for the keeper store
var ResourceDependencyMappingKey = 0x01

const (
	// ModuleName defines the module name
	ModuleName = "accesscontrol"

	QuerierRoute = ModuleName

	StoreKey = ModuleName
)

func GetResourceKey(moduleName string, messageType string) []byte {
	tempKey := append([]byte{byte(ResourceDependencyMappingKey)}, []byte(moduleName)...)
	return append(tempKey, []byte(messageType)...)
}

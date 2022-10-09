package types

// ResourceDependencyMappingKey is the key used for the keeper store
var ResourceDependencyMappingKey = 0x01

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

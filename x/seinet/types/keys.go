package types

const (
	// ModuleName defines the module name
	ModuleName = "seinet"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's routing key
	RouterKey = ModuleName

	// QuerierRoute defines the module's gRPC query route
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_seinet"
)

const (
	SeinetVaultAccount   = "seinet_vault"
	SeinetRoyaltyAccount = "seinet_royalty"
)

const (
	// CovenantKeyPrefix is the key prefix for covenant-related storage.
	CovenantKeyPrefix = "covenant:"
)

// CovenantKey returns the full store key for a given covenant ID.
func CovenantKey(id string) []byte {
	return []byte(CovenantKeyPrefix + id)
}

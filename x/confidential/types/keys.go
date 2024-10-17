package types

// TODO: Remove keys that are eventually not required
const (
	// ModuleName defines the module name
	ModuleName = "confidential"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey is the message route for slashing
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_confidential"
)

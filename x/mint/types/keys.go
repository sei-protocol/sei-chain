package types

// MinterKey is the key to use for the keeper store.
var MinterKey = []byte{0x00}

const (
	// module name
	ModuleName = "mint"

	// StoreKey is the default store key for mint
	StoreKey = ModuleName

	RouterKey = ModuleName

	MemStoreKey = "mem_mint"

	// QuerierRoute is the querier route for the minting store.
	QuerierRoute = StoreKey

	// Query endpoints supported by the minting querier
	QueryParameters = "parameters"
	QueryMinter     = "minter"

	// Format used for scheduling token releases
	/*#nosec G101 Not a hard coded credential*/
	TokenReleaseDateFormat = "2006-01-02"
)

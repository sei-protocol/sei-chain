package types

// MinterKey is the key to use for the keeper store.
var MinterKey = []byte{0x00}

// LastHalvenEpochKey is the key to use for the keeper store.
var LastHalvenEpochKey = []byte{0x03}

const (
	// module name
	ModuleName = "mint"

	// StoreKey is the default store key for mint
	StoreKey = ModuleName

	// QuerierRoute is the querier route for the minting store.
	QuerierRoute = StoreKey

	// Query endpoints supported by the minting querier
	QueryParameters       = "parameters"
	QueryInflation        = "inflation"
	QueryAnnualProvisions = "annual_provisions"
)

package types

// MinterKey is the key to use for the keeper store.
var MinterKey = []byte{0x00}

// LastTokenReleaseDate is the key to use for when the last token release was done.
var LastTokenReleaseDate = []byte{0x03}

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

	// Format used for scheduling token releases
	TokenReleaseDateFormat = "2006-01-02"
)

package types

// Module-level constants for x/seinet
const (
	// ModuleName defines the module name
	ModuleName = "seinet"

	// StoreKey is the primary module store key
	StoreKey = ModuleName

	// RouterKey is the message route for the module
	RouterKey = ModuleName

	// QuerierRoute defines the query routing key
	QuerierRoute = ModuleName

	// SeinetRoyaltyAccount is the name of the module account
	// used to hold and distribute royalties.
	SeinetRoyaltyAccount = "seinet_royalty"
)

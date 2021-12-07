package types

import (
	"fmt"
)

const (
	// ModuleName defines the interchain accounts module name
	ModuleName = "interchainaccounts"

	// VersionPrefix defines the current version for interchain accounts
	VersionPrefix = "ics27-1"

	// PortID is the default port id that the interchain accounts module binds to
	PortID = "interchain-account"

	// StoreKey is the store key string for interchain accounts
	StoreKey = ModuleName

	// RouterKey is the message route for interchain accounts
	RouterKey = ModuleName

	// QuerierRoute is the querier route for interchain accounts
	QuerierRoute = ModuleName

	// Delimiter is the delimiter used for the interchain accounts version string
	Delimiter = "."
)

var (
	// ActiveChannelKeyPrefix defines the key prefix used to store active channels
	ActiveChannelKeyPrefix = "activeChannel"

	// OwnerKeyPrefix defines the key prefix used to store interchain accounts
	OwnerKeyPrefix = "owner"

	// PortKeyPrefix defines the key prefix used to store ports
	PortKeyPrefix = "port"
)

// KeyActiveChannel creates and returns a new key used for active channels store operations
func KeyActiveChannel(portID string) []byte {
	return []byte(fmt.Sprintf("%s/%s", ActiveChannelKeyPrefix, portID))
}

// KeyOwnerAccount creates and returns a new key used for interchain account store operations
func KeyOwnerAccount(portID string) []byte {
	return []byte(fmt.Sprintf("%s/%s", OwnerKeyPrefix, portID))
}

// KeyPort creates and returns a new key used for port store operations
func KeyPort(portID string) []byte {
	return []byte(fmt.Sprintf("%s/%s", PortKeyPrefix, portID))
}

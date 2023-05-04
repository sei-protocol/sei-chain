package types

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ModuleName is the name of the oracle module
	ModuleName = "oracle"

	// StoreKey is the string store representation
	StoreKey = ModuleName

	// RouterKey is the msg router key for the oracle module
	RouterKey = ModuleName

	// QuerierRoute is the query router key for the oracle module
	QuerierRoute = ModuleName
)

// Keys for oracle store
// Items are stored with the following key: values
//
// - 0x01<denom_Bytes>: sdk.Dec
//
// - 0x02<valAddress_Bytes>: accAddress
//
// - 0x03<valAddress_Bytes>: int64
//
// - 0x04<valAddress_Bytes>: DEPRECATED: AggregateExchangeRatePrevote
//
// - 0x05<valAddress_Bytes>: AggregateExchangeRateVote
//
// - 0x06<denom_Bytes>: sdk.Dec
var (
	// Keys for store prefixes
	ExchangeRateKey       = []byte{0x01} // prefix for each key to a rate
	FeederDelegationKey   = []byte{0x02} // prefix for each key to a feeder delegation
	VotePenaltyCounterKey = []byte{0x03} // prefix for each key to a miss counter
	// DEPRECATED AggregateExchangeRatePrevoteKey = []byte{0x04}
	AggregateExchangeRateVoteKey = []byte{0x05} // prefix for each key to a aggregate vote
	VoteTargetKey                = []byte{0x06} // prefix for each key to a vote target
	PriceSnapshotKey             = []byte{0x07} // key for price snapshots history
)

// GetExchangeRateKey - stored by *denom*
func GetExchangeRateKey(denom string) []byte {
	return append(ExchangeRateKey, []byte(denom)...)
}

// GetFeederDelegationKey - stored by *Validator* address
func GetFeederDelegationKey(v sdk.ValAddress) []byte {
	return append(FeederDelegationKey, address.MustLengthPrefix(v)...)
}

// GetVotePenaltyCounterKey - stored by *Validator* address
func GetVotePenaltyCounterKey(v sdk.ValAddress) []byte {
	return append(VotePenaltyCounterKey, address.MustLengthPrefix(v)...)
}

// GetAggregateExchangeRateVoteKey - stored by *Validator* address
func GetAggregateExchangeRateVoteKey(v sdk.ValAddress) []byte {
	return append(AggregateExchangeRateVoteKey, address.MustLengthPrefix(v)...)
}

func GetVoteTargetKey(d string) []byte {
	return append(VoteTargetKey, []byte(d)...)
}

func ExtractDenomFromVoteTargetKey(key []byte) (denom string) {
	denom = string(key[1:])
	return
}

func GetKeyForTimestamp(timestamp uint64) []byte {
	timestampKey := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampKey, timestamp)
	return timestampKey
}

func GetPriceSnapshotKey(timestamp uint64) []byte {
	return append(PriceSnapshotKey, GetKeyForTimestamp(timestamp)...)
}

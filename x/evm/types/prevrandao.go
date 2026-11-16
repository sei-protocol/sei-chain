package types

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// PrevRandao returns the PREVRANDAO value of a block with the given time: the Keccak-256 hash of
// the binary encoding of the time in UTC. Its output is part of consensus for both x/evm and giga
// execution, and is the same for every time zone the instant is expressed in.
func PrevRandao(blockTime time.Time) common.Hash {
	// MarshalBinary encodes the location, and sdk.Context always holds block times in time.UTC, so
	// every other zone is converted first. A UTC time has no zone offset to encode, so the
	// encoding cannot fail.
	bz, _ := blockTime.UTC().MarshalBinary()
	return crypto.Keccak256Hash(bz)
}

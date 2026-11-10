package types

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// PrevRandao returns the PREVRANDAO value of a block with the given time: the Keccak-256 hash of
// blockTime.MarshalBinary(). Its output is part of consensus for both x/evm and giga execution.
// It hashes blockTime exactly as given, location included, so callers must pass a time.UTC time.
func PrevRandao(blockTime time.Time) (common.Hash, error) {
	bz, err := blockTime.MarshalBinary()
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(bz), nil
}

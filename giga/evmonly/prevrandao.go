package evmonly

import (
	"encoding/binary"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// PrevRandao returns the PREVRANDAO of a block with the given time: the Keccak-256 hash of the time
// in Unix nanoseconds, as a big-endian uint64. It is a hash of the proposer-chosen block time, not
// randomness. blockTime must not be before the Unix epoch.
func PrevRandao(blockTime time.Time) common.Hash {
	nanos := uint64(blockTime.UnixNano()) //nolint:gosec // G115: block times are not before the Unix epoch.
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, nanos))
}

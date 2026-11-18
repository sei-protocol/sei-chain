package receipthash

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/giga/apphash/merkle"
)

// maxLogTopics is the most topics an EVM log carries, and the number of topic slots in a log leaf.
const maxLogTopics = 4

// appendLogLeaf appends the preimage of a log's leaf in its receipt's logs tree to dst and returns the extended
// slice.
func appendLogLeaf(
	// The bytes to append to.
	dst []byte,
	// The account that emitted the log.
	address common.Address,
	// The log's topics. Must hold at most maxLogTopics.
	topics []common.Hash,
	// The log's data.
	data []byte,
) []byte {
	dst = append(dst, address[:]...)
	dst = append(dst, uint8(len(topics))) //nolint:gosec // G115 - validateLog() bounds the topic count
	for _, topic := range topics {
		dst = append(dst, topic[:]...)
	}
	var emptySlot common.Hash
	for range maxLogTopics - len(topics) {
		dst = append(dst, emptySlot[:]...)
	}
	return append(dst, data...)
}

// logLeafHashes returns the leaf hash of each log in its receipt's logs tree, in order, and scratch reused to hold leaf
// preimages, for the caller to pass to the next call.
func logLeafHashes(
	// The receipt's logs, in emission order. Each must have passed validateLog().
	logs []*ethtypes.Log,
	// A buffer whose contents are discarded and whose capacity is reused. May be nil.
	scratch []byte,
) ([][32]byte, []byte) {
	leafHashes := make([][32]byte, len(logs))
	for i, log := range logs {
		scratch = appendLogLeaf(scratch[:0], log.Address, log.Topics, log.Data)
		leafHashes[i] = merkle.MerkleLeafHash(scratch)
	}
	return leafHashes, scratch
}

// validateLog returns an error unless log is non-nil and has at most maxLogTopics topics.
func validateLog(
	// The log to check.
	log *ethtypes.Log,
) error {
	if log == nil {
		return fmt.Errorf("log is nil")
	}
	if len(log.Topics) > maxLogTopics {
		return fmt.Errorf("log has %d topics, more than %d", len(log.Topics), maxLogTopics)
	}
	return nil
}

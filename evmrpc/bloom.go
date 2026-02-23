package evmrpc

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/evmrpc/ethbloom"
)

// BloomIndexes is re-exported for backward compatibility.
type BloomIndexes = ethbloom.BloomIndexes

var BitMasks = [8]uint8{1, 2, 4, 8, 16, 32, 64, 128}

func EncodeFilters(addresses []common.Address, topics [][]common.Hash) [][]BloomIndexes {
	return ethbloom.EncodeFilters(addresses, topics)
}

func MatchFilters(bloom ethtypes.Bloom, filters [][]BloomIndexes) bool {
	return ethbloom.MatchFilters(bloom, filters)
}

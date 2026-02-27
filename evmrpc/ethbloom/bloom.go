package ethbloom

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/filters"
)

var bitMasks = [8]uint8{1, 2, 4, 8, 16, 32, 64, 128}

// BloomIndexes represents the bit indexes inside the bloom filter that belong
// to some key.
type BloomIndexes [3]uint

func calcBloomIndexes(b []byte) BloomIndexes {
	b = crypto.Keccak256(b)

	var idxs BloomIndexes
	for i := 0; i < len(idxs); i++ {
		idxs[i] = (uint(b[2*i])<<8)&2047 + uint(b[2*i+1])
	}
	return idxs
}

// EncodeFilters builds bloom-index slices from filter criteria.
// Result semantics: AND on outer level, OR on mid level, AND on inner level (all 3 bits).
func EncodeFilters(addresses []common.Address, topics [][]common.Hash) (res [][]BloomIndexes) {
	filters := make([][][]byte, 1+len(topics))
	if len(addresses) > 0 {
		filter := make([][]byte, len(addresses))
		for i, address := range addresses {
			filter[i] = address.Bytes()
		}
		filters = append(filters, filter)
	}
	for _, topicList := range topics {
		filter := make([][]byte, len(topicList))
		for i, topic := range topicList {
			filter[i] = topic.Bytes()
		}
		filters = append(filters, filter)
	}
	for _, filter := range filters {
		if len(filter) == 0 {
			continue
		}
		bloomBits := make([]BloomIndexes, len(filter))
		for i, clause := range filter {
			if clause == nil {
				bloomBits = nil
				break
			}
			bloomBits[i] = calcBloomIndexes(clause)
		}
		if bloomBits != nil {
			res = append(res, bloomBits)
		}
	}
	return
}

// MatchFilters returns true when bloom matches all filter groups.
func MatchFilters(bloom ethtypes.Bloom, filterGroups [][]BloomIndexes) bool {
	for _, filter := range filterGroups {
		if !matchFilter(bloom, filter) {
			return false
		}
	}
	return true
}

func matchFilter(bloom ethtypes.Bloom, filter []BloomIndexes) bool {
	for _, possibility := range filter {
		if matchBloomIndexes(bloom, possibility) {
			return true
		}
	}
	return false
}

func matchBloomIndexes(bloom ethtypes.Bloom, idx BloomIndexes) bool {
	for _, bit := range idx {
		whichByte := bloom[ethtypes.BloomByteLength-1-bit/8]
		mask := bitMasks[bit%8]
		if whichByte&mask == 0 {
			return false
		}
	}
	return true
}

// MatchesCriteria checks if a log matches the filter criteria (exact match).
func MatchesCriteria(log *ethtypes.Log, crit filters.FilterCriteria) bool {
	if len(crit.Addresses) > 0 {
		found := false
		for _, addr := range crit.Addresses {
			if log.Address == addr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for i, topicList := range crit.Topics {
		if len(topicList) == 0 {
			continue
		}
		if i >= len(log.Topics) {
			return false
		}
		found := false
		for _, topic := range topicList {
			if log.Topics[i] == topic {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

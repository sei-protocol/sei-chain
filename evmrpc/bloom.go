package evmrpc

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var BitMasks = [8]uint8{1, 2, 4, 8, 16, 32, 64, 128}

// bloomIndexes represents the bit indexes inside the bloom filter that belong
// to some key.
type bloomIndexes [3]uint

// calcBloomIndexes returns the bloom filter bit indexes belonging to the given key.
func calcBloomIndexes(b []byte) bloomIndexes {
	b = crypto.Keccak256(b)

	var idxs bloomIndexes
	for i := 0; i < len(idxs); i++ {
		idxs[i] = (uint(b[2*i])<<8)&2047 + uint(b[2*i+1])
	}
	return idxs
}

// res: AND on outer level, OR on mid level, AND on inner level (i.e. all 3 bits)
func EncodeFilters(addresses []common.Address, topics [][]common.Hash) (res [][]bloomIndexes) {
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
		// Gather the bit indexes of the filter rule, special casing the nil filter
		if len(filter) == 0 {
			continue
		}
		bloomBits := make([]bloomIndexes, len(filter))
		for i, clause := range filter {
			if clause == nil {
				bloomBits = nil
				break
			}
			bloomBits[i] = calcBloomIndexes(clause)
		}
		// Accumulate the filter rules if no nil rule was within
		if bloomBits != nil {
			res = append(res, bloomBits)
		}
	}
	return
}

// MatchFilters checks whether all the supplied filter rules match the bloom
// filter. For large input slices the work is split into chunks and evaluated in
// parallel to speed up matching. The final result is deterministic regardless of
// execution order.
func MatchFilters(bloom ethtypes.Bloom, filters [][]bloomIndexes) bool {
	// For small filter sets, run sequentially to avoid goroutine overhead.
	workers := runtime.GOMAXPROCS(0)
	if len(filters) <= workers {
		for _, filter := range filters {
			if !matchFilter(bloom, filter) {
				return false
			}
		}
		return true
	}

	// Split filters into chunks and evaluate concurrently.
	chunkSize := (len(filters) + workers - 1) / workers
	var ok atomic.Bool
	ok.Store(true)

	var wg sync.WaitGroup
	for i := 0; i < len(filters); i += chunkSize {
		end := i + chunkSize
		if end > len(filters) {
			end = len(filters)
		}
		wg.Add(1)
		go func(sub [][]bloomIndexes) {
			defer wg.Done()
			for _, f := range sub {
				if !ok.Load() {
					return
				}
				if !matchFilter(bloom, f) {
					ok.Store(false)
					return
				}
			}
		}(filters[i:end])
	}

	wg.Wait()
	return ok.Load()
}

func matchFilter(bloom ethtypes.Bloom, filter []bloomIndexes) bool {
	for _, possibility := range filter {
		if matchBloomIndexes(bloom, possibility) {
			return true
		}
	}
	return false
}

func matchBloomIndexes(bloom ethtypes.Bloom, idx bloomIndexes) bool {
	for _, bit := range idx {
		// big endian
		whichByte := bloom[ethtypes.BloomByteLength-1-bit/8]
		mask := BitMasks[bit%8]
		if whichByte&mask == 0 {
			return false
		}
	}
	return true
}

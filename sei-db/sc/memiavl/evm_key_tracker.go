package memiavl

import (
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-db/proto"
)

const evmModuleName = "evm"

type EvmKeyTracker struct {
	mu              sync.Mutex
	keyUpdateCounts map[string]int64

	totalUniqueKeys int64
	bucketOnce      int64 // == 1
	bucket2To10     int64 // [2, 10]
	bucket11To100   int64 // [11, 100]
	bucketOver100   int64 // > 100
}

func NewEvmKeyTracker() *EvmKeyTracker {
	return &EvmKeyTracker{
		keyUpdateCounts: make(map[string]int64),
	}
}

// ProcessBlock extracts EVM keys from the changesets, updates cumulative
// frequency counters, and logs the per-block results.
func (t *EvmKeyTracker) ProcessBlock(version int64, changesets []*proto.NamedChangeSet) {
	var evmKeys [][]byte
	for _, cs := range changesets {
		if cs.Name != evmModuleName {
			continue
		}
		for _, kvPair := range cs.Changeset.Pairs {
			evmKeys = append(evmKeys, kvPair.Key)
		}
	}
	if len(evmKeys) == 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	blockUniqueKeys := 0
	seen := make(map[string]struct{}, len(evmKeys))

	for _, key := range evmKeys {
		keyStr := string(key)
		if _, dup := seen[keyStr]; dup {
			continue
		}
		seen[keyStr] = struct{}{}
		blockUniqueKeys++

		prev := t.keyUpdateCounts[keyStr]
		t.keyUpdateCounts[keyStr] = prev + 1
		t.moveBucket(prev, prev+1)
		if prev == 0 {
			t.totalUniqueKeys++
		}
	}

	fmt.Printf("[EvmKeyTracker] height=%d block_unique_evm_keys=%d total_unique_evm_keys=%d "+
		"updated_once=%d updated_2_to_10=%d updated_11_to_100=%d updated_over_100=%d\n",
		version, blockUniqueKeys, t.totalUniqueKeys,
		t.bucketOnce, t.bucket2To10, t.bucket11To100, t.bucketOver100)
}

func (t *EvmKeyTracker) moveBucket(oldCount, newCount int64) {
	if oldCount > 0 {
		switch {
		case oldCount == 1:
			t.bucketOnce--
		case oldCount <= 10:
			t.bucket2To10--
		case oldCount <= 100:
			t.bucket11To100--
		default:
			t.bucketOver100--
		}
	}
	switch {
	case newCount == 1:
		t.bucketOnce++
	case newCount <= 10:
		t.bucket2To10++
	case newCount <= 100:
		t.bucket11To100++
	default:
		t.bucketOver100++
	}
}

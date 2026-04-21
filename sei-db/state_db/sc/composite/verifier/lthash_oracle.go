package verifier

import (
	"bytes"
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

const ltHashBatchSize = 100000

// VerifyLtHashSelf implements Oracle 3: full-scan the FlatKV data DBs via
// RawGlobalIterator, recompute the global LtHash from scratch, and compare
// the 32-byte Checksum against store.CommittedRootHash().
//
// Intended to be called on a read-only snapshot opened at a specific version
// so it does not race with the writer goroutine. The verifier.workerLoop
// already opens a readonly store before calling this function.
func VerifyLtHashSelf(ctx context.Context, store flatkv.Store) error {
	_ = ctx

	iter := store.RawGlobalIterator()
	defer func() { _ = iter.Close() }()

	var (
		pairs   []lthash.KVPairWithLastValue
		numKeys uint64
	)

	result := lthash.New()

	if iter.First() {
		for iter.Valid() {
			key := make([]byte, len(iter.Key()))
			copy(key, iter.Key())
			value := make([]byte, len(iter.Value()))
			copy(value, iter.Value())

			pairs = append(pairs, lthash.KVPairWithLastValue{
				Key:   key,
				Value: value,
			})
			numKeys++

			if len(pairs) >= ltHashBatchSize {
				batchResult, _ := lthash.ComputeLtHash(nil, pairs)
				result.MixIn(batchResult)
				pairs = pairs[:0]
			}

			iter.Next()
		}
	}

	if err := iter.Error(); err != nil {
		return fmt.Errorf("lthash scan: iterator error after %d keys: %w", numKeys, err)
	}

	if len(pairs) > 0 {
		batchResult, _ := lthash.ComputeLtHash(nil, pairs)
		result.MixIn(batchResult)
	}

	recomputed := result.Checksum()
	stored := store.CommittedRootHash()

	if !bytes.Equal(recomputed[:], stored) {
		return fmt.Errorf(
			"lthash mismatch: scanned %d keys, recomputed=%x stored=%x",
			numKeys, recomputed[:], stored,
		)
	}

	logger.Info("Oracle 3: LtHash self-check passed",
		"keys", numKeys,
		"hash", fmt.Sprintf("%x", recomputed[:16]),
	)
	return nil
}

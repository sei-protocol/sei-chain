package verifier

import (
	"bytes"
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// WriteMismatch describes a single key where memiavl and flatkv disagreed
// immediately after ApplyChangeSets returned.
type WriteMismatch struct {
	Module     string
	Key        []byte
	Expected   []byte // memiavl value (or nil for delete)
	Actual     []byte // flatkv value (or nil for delete)
	FromDelete bool   // changeset flagged this as a delete
}

func (m WriteMismatch) String() string {
	return fmt.Sprintf(
		"module=%s key=%x delete=%v memiavl=%x flatkv=%x",
		m.Module, m.Key, m.FromDelete, m.Expected, m.Actual,
	)
}

// VerifyWrites implements Oracle 1. For every EVM pair in changesets it
// re-reads the key from both backends and returns all mismatches.
//
// Must be called with the same changesets that were just passed to
// CompositeCommitStore.ApplyChangeSets, after both ApplyChangeSets calls
// return successfully. Reads use the pending-writes path on both backends,
// so this runs before Commit persists either.
//
// Cost: O(writes_per_block) Get calls on each backend, no iteration.
func VerifyWrites(
	ctx context.Context,
	evmStore sctypes.CommitKVStore,
	flatkvStore flatkv.Store,
	changesets []*proto.NamedChangeSet,
) []WriteMismatch {
	if evmStore == nil || flatkvStore == nil {
		return nil
	}

	var mismatches []WriteMismatch
	for _, cs := range changesets {
		if cs == nil || cs.Name != keys.EVMStoreKey || cs.Changeset.Pairs == nil {
			continue
		}
		for _, pair := range cs.Changeset.Pairs {
			if pair == nil {
				continue
			}
			mem := evmStore.Get(pair.Key)
			flat, found := flatkvStore.Get(keys.EVMStoreKey, pair.Key)
			if !found {
				flat = nil
			}

			switch {
			case pair.Delete:
				// Both backends must report "not present" after a
				// delete; nil from memiavl and !found from flatkv.
				if mem == nil && flat == nil {
					continue
				}
			default:
				if bytes.Equal(mem, flat) {
					continue
				}
			}

			mismatches = append(mismatches, WriteMismatch{
				Module:     cs.Name,
				Key:        bytes.Clone(pair.Key),
				Expected:   bytes.Clone(mem),
				Actual:     bytes.Clone(flat),
				FromDelete: pair.Delete,
			})
		}
	}

	_ = ctx
	return mismatches
}

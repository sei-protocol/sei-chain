package flatkv

import (
	"bytes"
	"fmt"

	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// VerifyLtHash full-scans all four data DBs and checks the recomputed LtHash
// against the store's committedLtHash. Read-write stores with uncommitted
// ApplyChangeSets writes are rejected (the on-disk scan cannot see them).
//
// Buffers every KV in memory (peak RSS ~2-3x on-disk size) and is not
// cancellable. Intended for tests and offline maintenance / migration checks;
// not suitable for online verification of production-sized state.
func VerifyLtHash(s Store) error {
	cs, ok := s.(*CommitStore)
	if !ok {
		return fmt.Errorf("VerifyLtHash: unsupported store type %T", s)
	}
	return verifyLtHashInternal(cs)
}

func verifyLtHashInternal(cs *CommitStore) error {
	// A read-write store between ApplyChangeSets and Commit has
	// workingLtHash != committedLtHash. The full scan below reads only
	// persisted DB contents, so there is no way to validate the in-memory
	// pending state against disk here. Fail loudly rather than masquerade
	// a pending-writes situation as an integrity error.
	if !cs.readOnly && !cs.workingLtHash.Equal(cs.committedLtHash) {
		return fmt.Errorf(
			"VerifyLtHash: store has uncommitted writes at version %d; "+
				"commit or reopen readonly before verifying",
			cs.committedVersion,
		)
	}

	var pairs []lthash.KVPairWithLastValue

	for _, db := range []seidbtypes.KeyValueDB{cs.accountDB, cs.codeDB, cs.storageDB, cs.miscDB} {
		if db == nil {
			continue
		}
		iter, err := db.NewIter(&seidbtypes.IterOptions{})
		if err != nil {
			return fmt.Errorf("VerifyLtHash: open iterator: %w", err)
		}
		for ; iter.Valid(); iter.Next() {
			if ktype.IsMetaKey(iter.Key()) {
				continue
			}
			pairs = append(pairs, lthash.KVPairWithLastValue{
				Key:   bytes.Clone(iter.Key()),
				Value: bytes.Clone(iter.Value()),
			})
		}
		if err := iter.Error(); err != nil {
			_ = iter.Close()
			return fmt.Errorf("VerifyLtHash: iterator error: %w", err)
		}
		_ = iter.Close()
	}

	fullScan, _ := lthash.ComputeLtHash(nil, pairs)
	fullChecksum := fullScan.Checksum()

	// Full scan reflects on-disk (committed) state, so the only correct
	// reference is committedLtHash. workingLtHash may include uncommitted
	// ApplyChangeSets updates that have not yet been persisted.
	committed := cs.committedLtHash.Checksum()

	if fullChecksum != committed {
		return fmt.Errorf(
			"VerifyLtHash: mismatch at version %d\n  committed: %x\n  full-scan: %x",
			cs.committedVersion, committed, fullChecksum,
		)
	}
	return nil
}

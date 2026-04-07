package flatkv

import (
	"bytes"
	"fmt"

	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// VerifyLtHash performs a full-scan recomputation of the LtHash over all four
// data DBs and compares the result against the store's current RootHash.
// Returns nil when they match; otherwise a descriptive error.
//
// The store should be opened (readonly or read-write) at the version to verify.
func VerifyLtHash(s Store) error {
	cs, ok := s.(*CommitStore)
	if !ok {
		return fmt.Errorf("VerifyLtHash: unsupported store type %T", s)
	}
	return verifyLtHashInternal(cs)
}

func verifyLtHashInternal(cs *CommitStore) error {
	var pairs []lthash.KVPairWithLastValue

	for _, db := range []seidbtypes.KeyValueDB{cs.accountDB, cs.codeDB, cs.storageDB, cs.legacyDB} {
		if db == nil {
			continue
		}
		iter, err := db.NewIter(&seidbtypes.IterOptions{})
		if err != nil {
			return fmt.Errorf("VerifyLtHash: open iterator: %w", err)
		}
		for iter.First(); iter.Valid(); iter.Next() {
			if isMetaKey(iter.Key()) {
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

	// Readonly stores have no pending batch: workingLtHash matches committedLtHash.
	incremental := cs.workingLtHash.Checksum()

	if fullChecksum != incremental {
		return fmt.Errorf(
			"VerifyLtHash: mismatch at version %d\n  incremental: %x\n  full-scan:   %x",
			cs.committedVersion, incremental, fullChecksum,
		)
	}
	return nil
}

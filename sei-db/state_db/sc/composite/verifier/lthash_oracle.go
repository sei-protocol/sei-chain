package verifier

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

// VerifyLtHashSelf implements Oracle 3: full-scan the FlatKV data DBs and
// compare the recomputed LtHash against the store's committed LtHash.
//
// TODO: port the test-only `verifyLtHashAtHeight` / `verifyLtHashConsistency`
// helpers in sei-db/state_db/sc/flatkv/{perdb_lthash_test.go,
// lthash_correctness_test.go} into a production, non-testing.T API that
// 1) iterates every key across the account/storage/code/legacy DBs via
//    RawGlobalIterator, 2) recomputes the global LtHash from scratch, and
//    3) compares the 32-byte Checksum against store.CommittedRootHash().
// Until that helper exists, this oracle is a no-op so the verifier package
// keeps compiling on the shadow branch. Oracle 1 (write-time diff), Oracle 2
// (forward-subset scan), and Oracle 4 (historical checkpoint diff) provide
// overlapping coverage of the same invariant in the meantime.
func VerifyLtHashSelf(ctx context.Context, store flatkv.Store) error {
	_ = ctx
	_ = store
	return nil
}

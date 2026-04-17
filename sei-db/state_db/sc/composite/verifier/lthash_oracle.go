package verifier

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

// VerifyLtHashSelf implements Oracle 3: full-scan the FlatKV data DBs and
// compare the recomputed LtHash against the store's committedLtHash.
// The caller must pass a store that is either read-only or has no pending
// ApplyChangeSets writes; flatkv.VerifyLtHash enforces this.
func VerifyLtHashSelf(ctx context.Context, store flatkv.Store) error {
	_ = ctx
	return flatkv.VerifyLtHash(store)
}

// Package consistency holds cross-storage invariant checks that run after
// every storage component has loaded its persisted state but before block
// production begins. The checks are consensus-agnostic: the consensus engine
// supplies its persisted record, the storage layer supplies its current view,
// and this package compares them. Catching a divergence here prevents the
// node from silently committing on top of corrupted local state.
package consistency

import (
	"bytes"
	"fmt"
)

// VerifyAppHash reports whether the AppHash that the consensus engine
// persisted for the most recent committed block matches the AppHash that the
// application currently computes from its loaded storage. A mismatch means
// the node's storage diverged from the chain — typically a partial wipe of
// state_commit while the consensus log was retained — and the caller should
// refuse to start.
//
// version names the block both hashes refer to and is included verbatim in
// the error so the operator can correlate with the chain head.
//
// Returns nil when either input is empty: a fresh chain has no persisted
// AppHash yet, and an interrupted state-sync may leave the application's
// hash uncomputed. The check is intentionally permissive on those paths so
// it does not block recovery; the strict comparison only fires when both
// witnesses exist and disagree.
func VerifyAppHash(persisted, current []byte, version int64) error {
	if len(persisted) == 0 || len(current) == 0 {
		return nil
	}
	if bytes.Equal(persisted, current) {
		return nil
	}
	return fmt.Errorf(
		"AppHash divergence at version %d: application reports %X, consensus log has %X; "+
			"local storage is inconsistent with the chain (likely a partial wipe of state_commit); "+
			"recover via state-sync from a healthy peer",
		version, current, persisted)
}

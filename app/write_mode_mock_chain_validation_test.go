//go:build mock_chain_validation

package app

import (
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// See write_mode_default_test.go for what these four values are and why each
// build states them separately. This build declares sc-write-mode-enable-auto
// false, so an explicit sc-write-mode is honored rather than replaced by auto.

const stateCommitRecord = "state-commit.reserve"

const wantAbsentAutoWriteMode = sctypes.MemiavlOnly

// scWriteModeDivergence is empty here. The section declares memiavl_only and a
// node missing the key runs memiavl_only, so this is the one build on which
// sc-write-mode is not a setting whose declared and running values disagree.
var scWriteModeDivergence []string

const scWriteModeANodeRuns = "memiavl_only"

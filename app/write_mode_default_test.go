//go:build !mock_chain_validation

package app

import (
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// The four values below are everything in this package's configuration records
// that moves with sc-write-mode-enable-auto's in-code default. The reserve build
// declares that key false where this one declares it true, so it carries a
// write_mode_mock_chain_validation_test.go stating each of these the other way.

// stateCommitRecord names this build's [state-commit] defaults record. The two
// builds keep separate records because a shared one would be rewritten by
// whichever build regenerated it last, losing the value the reviewer needed.
const stateCommitRecord = "state-commit"

// wantAbsentAutoWriteMode is the mode parseSCConfigs resolves memiavl_only to
// when app.toml carries no sc-write-mode-enable-auto key.
const wantAbsentAutoWriteMode = sctypes.Auto

// scWriteModeDivergence is the tail of every theDivergences entry. sc-write-mode
// belongs on those lists here, because the section declares memiavl_only and a
// node missing the key runs auto instead.
var scWriteModeDivergence = []string{FlagSCWriteMode}

// scWriteModeANodeRuns is what sc-write-mode resolves to for a file carrying no
// keys at all.
const scWriteModeANodeRuns = "auto"

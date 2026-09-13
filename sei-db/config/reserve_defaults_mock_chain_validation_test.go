//go:build mock_chain_validation

package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

func TestDefaultStateCommitConfigWriteModeOnAReserveBuild(t *testing.T) {
	cfg := DefaultStateCommitConfig()
	// The raw default is the same fixed fallback as the stock build. Only
	// WriteModeEnableAuto moves, and at false ApplyWriteModeAuto leaves the
	// fallback standing instead of replacing it with auto.
	require.Equal(t, types.MemiavlOnly, cfg.WriteMode)
	require.False(t, cfg.WriteModeEnableAuto)
}

// TestStateCommitConfigTemplateRendersWriteModeEnableAuto records that this build
// writes the key the stock template omits. Rendering it is what keeps a generated
// app.toml truthful: the two builds default the key differently, so an absent key
// would describe the wrong node on one of them.
func TestStateCommitConfigTemplateRendersWriteModeEnableAuto(t *testing.T) {
	require.NotEmpty(t, reserveStateCommitConfigTemplate)
	require.Contains(t, renderedAssignments(t), "sc-write-mode-enable-auto")
	require.Contains(t, renderedAssignments(t), "sc-write-mode")
}

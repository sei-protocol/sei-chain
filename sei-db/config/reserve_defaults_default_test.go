//go:build !mock_chain_validation

package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

func TestDefaultStateCommitConfigWriteMode(t *testing.T) {
	cfg := DefaultStateCommitConfig()
	// The raw default is the fixed fallback; auto comes from WriteModeEnableAuto
	// via ApplyWriteModeAuto at the config-parse boundary.
	require.Equal(t, types.MemiavlOnly, cfg.WriteMode)
	require.True(t, cfg.WriteModeEnableAuto)
}

// TestStateCommitConfigTemplateOmitsWriteModeEnableAuto records that a stock
// app.toml carries no sc-write-mode-enable-auto key. An operator who wants to pin
// a mode has to add the key by hand; the key's absence is what makes an unedited
// config resolve to auto. The template does name the key in a comment, so the
// assertion is on assignments in the rendered file rather than on the text.
func TestStateCommitConfigTemplateOmitsWriteModeEnableAuto(t *testing.T) {
	require.Empty(t, reserveStateCommitConfigTemplate)
	require.NotContains(t, renderedAssignments(t), "sc-write-mode-enable-auto")
	require.Contains(t, renderedAssignments(t), "sc-write-mode",
		"sc-write-mode itself is rendered, which is why its companion key reads as a defaulted one")
}

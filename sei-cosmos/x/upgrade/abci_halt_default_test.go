//go:build !mock_chain_validation && !mock_block_validation

// BeginBlocker panics when the binary carries a handler for an upgrade height
// the chain has not reached only in the default build; a mock validation build
// swallows ErrUpgradeBeforeTrigger to let a replay run past it, so this halt is
// default-build only.
package upgrade_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

func TestHaltIfTooNew(t *testing.T) {
	s := setupTest(t, 10)
	t.Log("Verify that we don't panic with registered plan not in database at all")
	var called int
	s.keeper.SetUpgradeHandler("future", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		called++
		return vm, nil
	})

	newCtx := s.ctx.WithBlockHeight(s.ctx.BlockHeight() + 1).WithBlockTime(time.Now())
	require.NotPanics(t, func() {
		upgrade.BeginBlocker(s.keeper, newCtx)
	})
	require.Equal(t, 0, called)

	t.Log("Verify we panic if we have a registered handler ahead of time")
	err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "future", Height: s.ctx.BlockHeight() + 3}})
	require.NoError(t, err)
	require.Panics(t, func() {
		upgrade.BeginBlocker(s.keeper, newCtx)
	})
	require.Equal(t, 0, called)

	t.Log("Verify we no longer panic if the plan is on time")

	futCtx := s.ctx.WithBlockHeight(s.ctx.BlockHeight() + 3).WithBlockTime(time.Now())
	require.NotPanics(t, func() {
		upgrade.BeginBlocker(s.keeper, futCtx)
	})
	require.Equal(t, 1, called)

	VerifyCleared(t, futCtx)
}

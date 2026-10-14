package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestAuditDelegationByValIndexPasses(t *testing.T) {
	_, app, ctx := createTestInput(t)
	addrDels, valAddrs := generateAddresses(app, ctx, 3)
	for i := range addrDels {
		app.StakingKeeper.SetDelegation(ctx, types.NewDelegation(addrDels[i], valAddrs[i%len(valAddrs)], sdk.NewDec(1)))
	}

	audit := app.StakingKeeper.AuditDelegationByValIndex(ctx)
	require.Positive(t, audit.Delegations)
	require.Equal(t, audit.Delegations, audit.IndexEntries)
	require.Zero(t, audit.Missing)
	require.Zero(t, audit.Orphaned)
}

func TestAuditDelegationByValIndexFindsMissingAndOrphaned(t *testing.T) {
	_, app, ctx := createTestInput(t)
	addrDels, valAddrs := generateAddresses(app, ctx, 2)
	store := ctx.KVStore(app.StakingKeeper.GetStoreKey())

	// A delegation written without its index entry.
	seedDelegations(t, app, ctx, []types.Delegation{types.NewDelegation(addrDels[0], valAddrs[0], sdk.NewDec(1))})
	// An index entry whose delegation does not exist.
	store.Set(types.GetDelegationByValIndexKey(addrDels[1], valAddrs[1]), []byte{})

	audit := app.StakingKeeper.AuditDelegationByValIndex(ctx)
	require.Equal(t, 1, audit.Missing)
	require.Equal(t, 1, audit.Orphaned)
	require.Equal(t, []string{
		fmt.Sprintf("%X", types.GetDelegationKey(addrDels[0], valAddrs[0])),
	}, audit.MissingSamples)
	require.Equal(t, []string{
		fmt.Sprintf("%X", types.GetDelegationByValIndexKey(addrDels[1], valAddrs[1])),
	}, audit.OrphanedSamples)
}

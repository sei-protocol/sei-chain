package keeper_test

import (
	"testing"
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	gov "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestSlashDelegationUpdatesAreDeferredAndBounded(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: time.Unix(100, 0)})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})

	proposals := make([]govtypes.Proposal, 2)
	for i := range proposals {
		proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
		require.NoError(t, err)
		proposal.Status = govtypes.StatusVotingPeriod
		proposal.VotingEndTime = ctx.BlockTime().Add(time.Hour)
		app.GovKeeper.SetProposal(ctx, proposal)
		require.NoError(t, app.GovKeeper.AddVote(
			ctx,
			proposal.ProposalId,
			addrs[0],
			govtypes.NewNonSplitVoteOption(govtypes.OptionYes),
		))
		proposals[i] = proposal
	}

	store := ctx.KVStore(app.GetKey(govtypes.StoreKey))
	firstSnapshotKey := govtypes.VoteDelegationsKey(proposals[0].ProposalId, addrs[0])
	secondSnapshotKey := govtypes.VoteDelegationsKey(proposals[1].ProposalId, addrs[0])
	firstSnapshotBefore := append([]byte(nil), store.Get(firstSnapshotKey)...)
	secondSnapshotBefore := append([]byte(nil), store.Get(secondSnapshotKey)...)

	delegation, found := app.StakingKeeper.GetDelegation(ctx, addrs[0], valAddrs[0])
	require.True(t, found)
	updatedShares := delegation.Shares.Sub(sdk.OneDec())
	delegation.Shares = updatedShares
	app.StakingKeeper.SetDelegation(ctx, delegation)

	slashCtx := stakingtypes.WithSlashDelegationModification(ctx)
	app.GovKeeper.StakingHooks().AfterDelegationModified(slashCtx, addrs[0], valAddrs[0])

	require.True(t, app.GovKeeper.HasPendingVoteDelegationUpdates(ctx))
	require.Equal(t, firstSnapshotBefore, store.Get(firstSnapshotKey))
	require.Equal(t, secondSnapshotBefore, store.Get(secondSnapshotKey))
	requireVoteDelegationShares(t, app.GovKeeper.GetVoteDelegationSnapshots(ctx, proposals[0]), valAddrs[0], updatedShares)
	requireVoteDelegationShares(t, app.GovKeeper.GetVoteDelegationSnapshots(ctx, proposals[1]), valAddrs[0], updatedShares)

	complete, processed := app.GovKeeper.ProcessVoteDelegationUpdates(ctx, 1)
	require.False(t, complete)
	require.Equal(t, 1, processed)
	require.True(t, app.GovKeeper.HasPendingVoteDelegationUpdates(ctx))
	require.NotEqual(t, firstSnapshotBefore, store.Get(firstSnapshotKey))
	require.Equal(t, secondSnapshotBefore, store.Get(secondSnapshotKey))
	requireVoteDelegationShares(
		t,
		[]govtypes.VoteDelegationSnapshot{decodeVoteDelegationSnapshot(t, store.Get(firstSnapshotKey))},
		valAddrs[0],
		updatedShares,
	)
	requireVoteDelegationShares(t, app.GovKeeper.GetVoteDelegationSnapshots(ctx, proposals[0]), valAddrs[0], updatedShares)
	requireVoteDelegationShares(t, app.GovKeeper.GetVoteDelegationSnapshots(ctx, proposals[1]), valAddrs[0], updatedShares)

	complete, processed = app.GovKeeper.ProcessVoteDelegationUpdates(ctx, 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)
	require.False(t, app.GovKeeper.HasPendingVoteDelegationUpdates(ctx))
	require.NotEqual(t, secondSnapshotBefore, store.Get(secondSnapshotKey))
	requireVoteDelegationShares(
		t,
		[]govtypes.VoteDelegationSnapshot{decodeVoteDelegationSnapshot(t, store.Get(secondSnapshotKey))},
		valAddrs[0],
		updatedShares,
	)

	complete, processed, _, _, _ = app.GovKeeper.TallyIncremental(ctx, proposals[0], 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)
	archivedSnapshot := decodeVoteDelegationSnapshot(
		t,
		store.Get(govtypes.TallyVoteDelegationsKey(proposals[0].ProposalId, false, addrs[0])),
	)
	requireVoteDelegationShares(t, []govtypes.VoteDelegationSnapshot{archivedSnapshot}, valAddrs[0], updatedShares)
	require.False(t, store.Has(govtypes.VoteDelegationSnapshotRevisionKey(proposals[0].ProposalId, addrs[0])))
}

func TestTallySharesRecordBudgetWithCanonicalDelegationUpdates(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: time.Unix(100, 0)})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})

	secondValidator, found := app.StakingKeeper.GetValidator(ctx, valAddrs[1])
	require.True(t, found)
	_, err := app.StakingKeeper.Delegate(
		ctx,
		addrs[0],
		app.StakingKeeper.TokensFromConsensusPower(ctx, 1),
		stakingtypes.Unbonded,
		secondValidator,
		true,
	)
	require.NoError(t, err)

	proposal := newVotingProposalWithVote(t, ctx, app, addrs[0], ctx.BlockTime().Add(time.Hour))
	store := ctx.KVStore(app.GetKey(govtypes.StoreKey))
	snapshotKey := govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[0])
	originalSnapshot := decodeVoteDelegationSnapshot(t, store.Get(snapshotKey))

	updatedShares := make(map[string]sdk.Dec, 2)
	for _, validator := range valAddrs[:2] {
		delegation, found := app.StakingKeeper.GetDelegation(ctx, addrs[0], validator)
		require.True(t, found)
		delegation.Shares = delegation.Shares.Sub(sdk.OneDec())
		updatedShares[validator.String()] = delegation.Shares
		app.StakingKeeper.SetDelegation(ctx, delegation)
		app.GovKeeper.StakingHooks().AfterDelegationModified(
			stakingtypes.WithSlashDelegationModification(ctx),
			addrs[0],
			validator,
		)
	}

	require.True(t, app.GovKeeper.HasPendingVoteDelegationUpdates(ctx))
	effectiveSnapshots := app.GovKeeper.GetVoteDelegationSnapshots(ctx, proposal)
	genesisSnapshots := gov.ExportGenesis(ctx, app.GovKeeper).VoteDelegationSnapshots
	for _, validator := range valAddrs[:2] {
		requireVoteDelegationShares(t, effectiveSnapshots, validator, updatedShares[validator.String()])
		requireVoteDelegationShares(t, genesisSnapshots, validator, updatedShares[validator.String()])
	}

	complete, processed, _, _, _ := app.GovKeeper.TallyIncremental(ctx, proposal, 1)
	require.False(t, complete)
	require.Equal(t, 1, processed)
	require.True(t, app.GovKeeper.HasPendingVoteDelegationUpdates(ctx))
	partiallyUpdatedSnapshot := decodeVoteDelegationSnapshot(t, store.Get(snapshotKey))
	requireVoteDelegationShares(
		t,
		[]govtypes.VoteDelegationSnapshot{partiallyUpdatedSnapshot},
		valAddrs[0],
		updatedShares[valAddrs[0].String()],
	)
	requireVoteDelegationShares(
		t,
		[]govtypes.VoteDelegationSnapshot{partiallyUpdatedSnapshot},
		valAddrs[1],
		voteDelegationShares(t, originalSnapshot, valAddrs[1]),
	)

	complete, processed, _, _, _ = app.GovKeeper.TallyIncremental(ctx, proposal, 1)
	require.False(t, complete)
	require.Equal(t, 1, processed)
	require.False(t, app.GovKeeper.HasPendingVoteDelegationUpdates(ctx))
	require.True(t, app.GovKeeper.IsTallying(ctx, proposal.ProposalId))
	canonicalSnapshot := decodeVoteDelegationSnapshot(t, store.Get(snapshotKey))
	for _, validator := range valAddrs[:2] {
		requireVoteDelegationShares(
			t,
			[]govtypes.VoteDelegationSnapshot{canonicalSnapshot},
			validator,
			updatedShares[validator.String()],
		)
	}

	complete, processed, _, _, _ = app.GovKeeper.TallyIncremental(ctx, proposal, 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)
	archivedSnapshot := decodeVoteDelegationSnapshot(
		t,
		store.Get(govtypes.TallyVoteDelegationsKey(proposal.ProposalId, false, addrs[0])),
	)
	for _, validator := range valAddrs[:2] {
		requireVoteDelegationShares(
			t,
			[]govtypes.VoteDelegationSnapshot{archivedSnapshot},
			validator,
			updatedShares[validator.String()],
		)
	}
	require.False(t, store.Has(govtypes.VoteDelegationSnapshotRevisionKey(proposal.ProposalId, addrs[0])))
}

func TestSlashRedelegationDefersVoteSnapshotRefresh(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: time.Unix(100, 0)})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})
	proposal := newVotingProposalWithVote(t, ctx, app, addrs[0], ctx.BlockTime().Add(time.Hour))

	store := ctx.KVStore(app.GetKey(govtypes.StoreKey))
	snapshotKey := govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[0])
	snapshotBefore := append([]byte(nil), store.Get(snapshotKey)...)
	delegationBefore, found := app.StakingKeeper.GetDelegation(ctx, addrs[0], valAddrs[0])
	require.True(t, found)
	sourceValidator, found := app.StakingKeeper.GetValidator(ctx, valAddrs[1])
	require.True(t, found)
	redelegation := stakingtypes.NewRedelegation(
		addrs[0],
		valAddrs[1],
		valAddrs[0],
		ctx.BlockHeight(),
		ctx.BlockTime().Add(time.Hour),
		app.StakingKeeper.TokensFromConsensusPower(ctx, 5),
		delegationBefore.Shares,
	)

	app.StakingKeeper.SlashRedelegation(ctx, sourceValidator, redelegation, ctx.BlockHeight(), sdk.NewDecWithPrec(5, 1))
	delegationAfter, found := app.StakingKeeper.GetDelegation(ctx, addrs[0], valAddrs[0])
	require.True(t, found)
	require.True(t, delegationAfter.Shares.LT(delegationBefore.Shares))
	require.Equal(t, snapshotBefore, store.Get(snapshotKey))
	require.True(t, app.GovKeeper.HasPendingVoteDelegationUpdates(ctx))

	complete, processed := app.GovKeeper.ProcessVoteDelegationUpdates(ctx, 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)
	requireVoteDelegationShares(
		t,
		app.GovKeeper.GetVoteDelegationSnapshots(ctx, proposal),
		valAddrs[0],
		delegationAfter.Shares,
	)
}

func TestSlashDelegationRemovalFoldsIntoCanonicalSnapshot(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: time.Unix(100, 0)})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})
	proposal := newVotingProposalWithVote(t, ctx, app, addrs[0], ctx.BlockTime().Add(time.Hour))

	app.GovKeeper.StakingHooks().BeforeDelegationRemoved(
		stakingtypes.WithSlashDelegationModification(ctx),
		addrs[0],
		valAddrs[0],
	)
	complete, processed := app.GovKeeper.ProcessVoteDelegationUpdates(ctx, 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)

	store := ctx.KVStore(app.GetKey(govtypes.StoreKey))
	snapshot := decodeVoteDelegationSnapshot(
		t,
		store.Get(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[0])),
	)
	requireNoVoteDelegation(t, snapshot, valAddrs[0])
}

func TestSynchronousDelegationRefreshSupersedesDeferredSlashUpdate(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: time.Unix(100, 0)})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})
	proposal := newVotingProposalWithVote(t, ctx, app, addrs[0], ctx.BlockTime().Add(time.Hour))

	delegation, found := app.StakingKeeper.GetDelegation(ctx, addrs[0], valAddrs[0])
	require.True(t, found)
	delegation.Shares = delegation.Shares.Sub(sdk.OneDec())
	app.StakingKeeper.SetDelegation(ctx, delegation)
	app.GovKeeper.StakingHooks().AfterDelegationModified(
		stakingtypes.WithSlashDelegationModification(ctx),
		addrs[0],
		valAddrs[0],
	)

	latestShares := delegation.Shares.Sub(sdk.OneDec())
	delegation.Shares = latestShares
	app.StakingKeeper.SetDelegation(ctx, delegation)
	app.GovKeeper.StakingHooks().AfterDelegationModified(ctx, addrs[0], valAddrs[0])

	complete, processed := app.GovKeeper.ProcessVoteDelegationUpdates(ctx, 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)
	requireVoteDelegationShares(t, app.GovKeeper.GetVoteDelegationSnapshots(ctx, proposal), valAddrs[0], latestShares)
	store := ctx.KVStore(app.GetKey(govtypes.StoreKey))
	snapshot := decodeVoteDelegationSnapshot(
		t,
		store.Get(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[0])),
	)
	requireVoteDelegationShares(t, []govtypes.VoteDelegationSnapshot{snapshot}, valAddrs[0], latestShares)
}

func TestSlashDelegationUpdateHonorsVotingEndTime(t *testing.T) {
	for _, tc := range []struct {
		name        string
		offset      time.Duration
		applyUpdate bool
	}{
		{name: "at voting end", offset: 0, applyUpdate: true},
		{name: "after voting end", offset: time.Second, applyUpdate: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := seiapp.Setup(t, false, false, false)
			ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: time.Unix(100, 0)})
			addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})
			votingEnd := ctx.BlockTime().Add(time.Minute)
			proposal := newVotingProposalWithVote(t, ctx, app, addrs[0], votingEnd)

			delegation, found := app.StakingKeeper.GetDelegation(ctx, addrs[0], valAddrs[0])
			require.True(t, found)
			originalShares := delegation.Shares
			updatedShares := originalShares.Sub(sdk.OneDec())
			delegation.Shares = updatedShares
			app.StakingKeeper.SetDelegation(ctx, delegation)
			slashCtx := stakingtypes.WithSlashDelegationModification(ctx.WithBlockTime(votingEnd.Add(tc.offset)))
			app.GovKeeper.StakingHooks().AfterDelegationModified(slashCtx, addrs[0], valAddrs[0])

			complete, processed := app.GovKeeper.ProcessVoteDelegationUpdates(ctx, 1)
			require.True(t, complete)
			require.Equal(t, 1, processed)
			expectedShares := originalShares
			if tc.applyUpdate {
				expectedShares = updatedShares
			}
			requireVoteDelegationShares(
				t,
				app.GovKeeper.GetVoteDelegationSnapshots(ctx, proposal),
				valAddrs[0],
				expectedShares,
			)
		})
	}
}

func newVotingProposalWithVote(
	t *testing.T,
	ctx sdk.Context,
	app *seiapp.App,
	voter sdk.AccAddress,
	votingEnd time.Time,
) govtypes.Proposal {
	t.Helper()
	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	proposal.Status = govtypes.StatusVotingPeriod
	proposal.VotingEndTime = votingEnd
	app.GovKeeper.SetProposal(ctx, proposal)
	require.NoError(t, app.GovKeeper.AddVote(
		ctx,
		proposal.ProposalId,
		voter,
		govtypes.NewNonSplitVoteOption(govtypes.OptionYes),
	))
	return proposal
}

func decodeVoteDelegationSnapshot(t *testing.T, bz []byte) govtypes.VoteDelegationSnapshot {
	t.Helper()
	var snapshot govtypes.VoteDelegationSnapshot
	seiapp.MakeEncodingConfig().Marshaler.MustUnmarshal(bz, &snapshot)
	return snapshot
}

func voteDelegationShares(t *testing.T, snapshot govtypes.VoteDelegationSnapshot, validator sdk.ValAddress) sdk.Dec {
	t.Helper()
	for _, delegation := range snapshot.Delegations {
		if delegation.Validator == validator.String() {
			return delegation.Shares
		}
	}
	require.FailNow(t, "validator delegation not found", validator.String())
	return sdk.ZeroDec()
}

func requireNoVoteDelegation(t *testing.T, snapshot govtypes.VoteDelegationSnapshot, validator sdk.ValAddress) {
	t.Helper()
	for _, delegation := range snapshot.Delegations {
		require.NotEqual(t, validator.String(), delegation.Validator)
	}
}

func requireVoteDelegationShares(
	t *testing.T,
	snapshots []govtypes.VoteDelegationSnapshot,
	validator sdk.ValAddress,
	expected sdk.Dec,
) {
	t.Helper()
	require.Len(t, snapshots, 1)
	for _, delegation := range snapshots[0].Delegations {
		if delegation.Validator == validator.String() {
			require.True(t, delegation.Shares.Equal(expected), "%s != %s", delegation.Shares, expected)
			return
		}
	}
	require.Fail(t, "validator delegation not found", validator.String())
}

//go:build upgrade_v69

package app_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/upgradetest"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

const v69UpgradeName = "v6.9"

// v69RegisterPointerDisabledKey is the params-store key of the EVM parameter v6.9 deletes.
var v69RegisterPointerDisabledKey = []byte(evmtypes.ModuleName + "/KeyRegisterPointerDisabled")

func newV69Chain(t *testing.T) *processblock.App {
	t.Helper()
	t.Setenv("UPGRADE_VERSION_LIST", v69UpgradeName)
	app := processblock.NewTestApp(t)
	processblock.CommonPreset(app)
	app.RegisterUpgradeHandlers()
	return app
}

func applyV69(t *testing.T, app *processblock.App) {
	t.Helper()
	app.UpgradeKeeper.ApplyUpgrade(app.Ctx(), upgradetypes.Plan{
		Name:   v69UpgradeName,
		Height: app.Ctx().BlockHeight(),
	})
}

// TestV69Upgrade requires v6.9 to delete the stored register_pointer_disabled parameter
// and to leave the rest of the params store, the EVM parameters and the module version
// map unchanged.
func TestV69Upgrade(t *testing.T) {
	a := newV69Chain(t)
	ctx := a.Ctx()
	store := ctx.KVStore(a.GetKey(paramstypes.StoreKey))
	store.Set(v69RegisterPointerDisabledKey, []byte("true"))
	kept := v69StoreEntries(t, store)
	delete(kept, string(v69RegisterPointerDisabledKey))
	versions := a.UpgradeKeeper.GetModuleVersionMap(ctx)
	evmParams := a.EvmKeeper.GetParams(ctx)

	applyV69(t, a)

	require.Equal(t, kept, v69StoreEntries(t, store),
		"v6.9 changed the params store beyond deleting register_pointer_disabled")
	require.Equal(t, evmParams, a.EvmKeeper.GetParams(ctx))
	require.Equal(t, versions, a.UpgradeKeeper.GetModuleVersionMap(ctx), "v6.9 changed the module version map")
}

// TestV69RefusesQueuedEVMProposals requires the v6.9 handler to fail while a proposal
// routed to evm is still in its deposit or voting period, since gov resolves the route of
// a queued proposal once it passes and v6.9 registers none for evm.
func TestV69RefusesQueuedEVMProposals(t *testing.T) {
	for name, enqueue := range map[string]func(a *processblock.App, proposal govtypes.Proposal){
		"deposit period": func(a *processblock.App, proposal govtypes.Proposal) {
			a.GovKeeper.InsertInactiveProposalQueue(a.Ctx(), proposal.ProposalId, proposal.DepositEndTime)
		},
		"voting period": func(a *processblock.App, proposal govtypes.Proposal) {
			a.GovKeeper.InsertActiveProposalQueue(a.Ctx(), proposal.ProposalId, a.Ctx().BlockTime().Add(time.Hour))
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := newV69Chain(t)
			proposal := v69StorePointerProposal(t, a)
			enqueue(a, proposal)

			require.PanicsWithError(t, fmt.Sprintf(
				"governance proposals [%d] are still queued and route to %q, which this build no longer registers",
				proposal.ProposalId, evmtypes.RouterKey), func() { applyV69(t, a) })
		})
	}
}

// TestV69KeepsPointerProposalsReadable requires a pointer proposal whose vote has ended to
// still decode after v6.9.
func TestV69KeepsPointerProposalsReadable(t *testing.T) {
	a := newV69Chain(t)
	proposal := v69StorePointerProposal(t, a)
	proposal.Status = govtypes.StatusFailed
	a.GovKeeper.SetProposal(a.Ctx(), proposal)

	applyV69(t, a)

	stored, found := a.GovKeeper.GetProposal(a.Ctx(), proposal.ProposalId)
	require.True(t, found)
	require.Equal(t, govtypes.StatusFailed, stored.Status)
	require.Equal(t, proposal.GetContent(), stored.GetContent())
}

func TestV69CrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t, seedV68State, verifyV69State)
}

// seedV68State records the v6.8 version map and requires v6.8 to store the
// register_pointer_disabled parameter.
func seedV68State(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v69UpgradeName, chain.UpgradeName(t))
	require.NotEmpty(t, chain.QueryStore(t, paramstypes.StoreKey, v69RegisterPointerDisabledKey),
		"v6.8 does not store register_pointer_disabled")
	chain.Record(t, "module_versions", v69ModuleVersions(t, chain))
}

// verifyV69State requires every node to agree on the blocks around the upgrade, the module
// version map to be unchanged, and the register_pointer_disabled parameter to be gone.
func verifyV69State(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v69UpgradeName, chain.UpgradeName(t))

	appliedHeight := v69AppliedHeight(t, chain)
	require.Equal(t, chain.TargetHeight(t), appliedHeight)
	chain.Record(t, "applied_height", appliedHeight)
	// A header carries the application hash of the state its parent produced, so
	// the upgrade block's own result appears one height above the applied height.
	chain.RequireBlockAgreement(t, appliedHeight, appliedHeight+1, appliedHeight+2, appliedHeight+3)

	var before map[string]uint64
	chain.Replay(t, "module_versions", &before)
	after := v69ModuleVersions(t, chain)
	chain.Record(t, "module_versions_after", after)
	require.Equal(t, before, after, "v6.9 changed the module version map")
	require.Empty(t, chain.QueryStore(t, paramstypes.StoreKey, v69RegisterPointerDisabledKey),
		"v6.9 left register_pointer_disabled in the params store")
}

// v69StorePointerProposal stores a native pointer proposal in its deposit period without
// queueing it, bypassing the submission check that refuses one.
func v69StorePointerProposal(t *testing.T, a *processblock.App) govtypes.Proposal {
	t.Helper()
	ctx := a.Ctx()
	id, err := a.GovKeeper.GetProposalID(ctx)
	require.NoError(t, err)
	content := &evmtypes.AddERCNativePointerProposalV2{
		Title: "Add pointer", Description: "Add an ERC20 pointer", Token: "ufoo", Name: "FOO", Symbol: "FOO", Decimals: 6,
	}
	proposal, err := govtypes.NewProposal(content, id, ctx.BlockTime(), ctx.BlockTime().Add(time.Hour), false)
	require.NoError(t, err)
	a.GovKeeper.SetProposal(ctx, proposal)
	a.GovKeeper.SetProposalID(ctx, id+1)
	return proposal
}

func v69StoreEntries(t *testing.T, store sdk.KVStore) map[string][]byte {
	t.Helper()
	iterator := store.Iterator(nil, nil)
	defer func() { require.NoError(t, iterator.Close()) }()
	entries := map[string][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		entries[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	return entries
}

func v69ModuleVersions(t *testing.T, chain *upgradetest.CrossVersion) map[string]uint64 {
	t.Helper()
	output := chain.MustSeid(t, "", "q", "upgrade", "module_versions", "--output", "json")
	var response struct {
		ModuleVersions []struct {
			Name    string          `json:"name"`
			Version json.RawMessage `json:"version"`
		} `json:"module_versions"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &response), output)
	versions := make(map[string]uint64, len(response.ModuleVersions))
	for _, entry := range response.ModuleVersions {
		version, err := strconv.ParseUint(strings.Trim(string(entry.Version), `"`), 10, 64)
		require.NoError(t, err, "module %s has version %s", entry.Name, entry.Version)
		versions[entry.Name] = version
	}
	return versions
}

func v69AppliedHeight(t *testing.T, chain *upgradetest.CrossVersion) int64 {
	t.Helper()
	output := chain.MustSeid(t, "", "q", "upgrade", "applied", v69UpgradeName, "--output", "json")
	var applied struct {
		Header struct {
			Height json.RawMessage `json:"height"`
		} `json:"header"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &applied), output)
	height, err := strconv.ParseInt(strings.Trim(string(applied.Header.Height), `"`), 10, 64)
	require.NoError(t, err, "applied height %s", applied.Header.Height)
	return height
}

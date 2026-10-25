package keeper

import (
	"encoding/json"
	"fmt"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

func setContractExecutionDisabled(ctx sdk.Context, keepers TestKeepers, disabled bool) {
	params := types.DefaultParams()
	params.ContractExecutionDisabled = disabled
	keepers.WasmKeeper.SetParams(ctx, params)
}

// executeThroughKeeper has owner release escrow through the permissioned keeper, the route
// the EVM wasmd and solo precompiles take.
func (f executeFixture) executeThroughKeeper(t *testing.T, ctx sdk.Context, keepers TestKeepers) error {
	t.Helper()
	_, err := keepers.ContractKeeper.Execute(ctx, f.escrow, f.owner, hackatomRelease, nil)
	return err
}

// executeByProposal releases escrow as owner through an ExecuteContractProposal.
func (f executeFixture) executeByProposal(t *testing.T, ctx sdk.Context, keepers TestKeepers) error {
	t.Helper()
	return NewWasmProposalHandler(keepers.WasmKeeper, types.EnableAllProposals)(ctx, &types.ExecuteContractProposal{
		Title:       "Release the escrow",
		Description: "Release the escrow",
		RunAs:       f.owner.String(),
		Contract:    f.escrow.String(),
		Msg:         hackatomRelease,
	})
}

// instantiateEscrow has owner instantiate another hackatom escrow paying beneficiary.
func (f executeFixture) instantiateEscrow(t *testing.T, ctx sdk.Context, keepers TestKeepers) error {
	t.Helper()
	initMsg, err := json.Marshal(HackatomExampleInitMsg{Verifier: f.owner, Beneficiary: f.beneficiary})
	require.NoError(t, err)
	_, _, err = keepers.ContractKeeper.Instantiate(ctx, f.hackatomID, f.owner, nil, initMsg, "another escrow", nil)
	return err
}

// migrateEscrow has owner, the admin of escrow, migrate it to beneficiary as verifier.
func (f executeFixture) migrateEscrow(t *testing.T, ctx sdk.Context, keepers TestKeepers) error {
	t.Helper()
	migrateMsg, err := json.Marshal(struct {
		Verifier sdk.AccAddress `json:"verifier"`
	}{Verifier: f.beneficiary})
	require.NoError(t, err)
	_, err = keepers.ContractKeeper.Migrate(ctx, f.escrow, f.owner, f.hackatomID, migrateMsg)
	return err
}

// sudoEscrow has a module move funds out of escrow to beneficiary through sudo.
func (f executeFixture) sudoEscrow(t *testing.T, ctx sdk.Context, keepers TestKeepers) error {
	t.Helper()
	msg, err := json.Marshal(sudoMsg{StealFunds: stealFundsMsg{
		Recipient: f.beneficiary.String(),
		Amount:    wasmvmtypes.Coins{wasmvmtypes.NewCoin(1, "denom")},
	}})
	require.NoError(t, err)
	_, err = keepers.WasmKeeper.Sudo(ctx, f.escrow, msg)
	return err
}

// storeEntries returns every entry of every store in the test multistore, by store name.
func storeEntries(t *testing.T, ctx sdk.Context, keepers TestKeepers) map[string]map[string]string {
	t.Helper()
	entries := map[string]map[string]string{}
	for _, storeKey := range keepers.MultiStore.StoreKeys() {
		iterator := ctx.KVStore(storeKey).Iterator(nil, nil)
		stored := map[string]string{}
		for ; iterator.Valid(); iterator.Next() {
			stored[string(iterator.Key())] = string(iterator.Value())
		}
		require.NoError(t, iterator.Close())
		entries[storeKey.Name()] = stored
	}
	return entries
}

// TestContractExecutionDisabled runs a contract through every entry point, executing it from
// every origin, with the contractExecutionDisabled param on and then off.
func TestContractExecutionDisabled(t *testing.T) {
	calls := map[string]func(executeFixture, *testing.T, sdk.Context, TestKeepers) error{
		"MsgExecuteContract":               executeFixture.sendDirectly,
		"execute dispatched by a contract": executeFixture.dispatchFromContract,
		"execute through the keeper":       executeFixture.executeThroughKeeper,
		"ExecuteContractProposal":          executeFixture.executeByProposal,
		"instantiate":                      executeFixture.instantiateEscrow,
		"migrate":                          executeFixture.migrateEscrow,
		"sudo":                             executeFixture.sudoEscrow,
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			ctx, keepers := CreateTestInput(t, false, ReflectFeatures)
			f := newExecuteFixture(t, ctx, keepers)

			setContractExecutionDisabled(ctx, keepers, true)
			before := storeEntries(t, ctx, keepers)
			require.NotEmpty(t, before[types.StoreKey], "the snapshot holds no contract state")
			require.ErrorIs(t, call(f, t, ctx, keepers), types.ErrContractExecutionDisabled)
			require.Equal(t, before, storeEntries(t, ctx, keepers), "the refused call changed state")

			setContractExecutionDisabled(ctx, keepers, false)
			require.NoError(t, call(f, t, ctx, keepers))
		})
	}
}

// TestContractExecutionDisabledLeavesQueriesOpen queries a contract with the
// contractExecutionDisabled param on.
func TestContractExecutionDisabledLeavesQueriesOpen(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures)
	f := newExecuteFixture(t, ctx, keepers)
	setContractExecutionDisabled(ctx, keepers, true)

	res, err := keepers.WasmKeeper.QuerySmart(ctx, f.escrow, []byte(`{"verifier":{}}`))
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{"verifier":"%s"}`, f.owner), string(res))
}

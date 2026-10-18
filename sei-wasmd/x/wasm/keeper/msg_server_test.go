package keeper

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

var hackatomRelease = []byte(`{"release":{}}`)

// executeFixture holds two funded hackatom escrows paying beneficiary: escrow, released by
// owner, and reflectEscrow, released by a reflect contract that owner owns.
type executeFixture struct {
	owner         sdk.AccAddress
	beneficiary   sdk.AccAddress
	escrow        sdk.AccAddress
	reflect       sdk.AccAddress
	reflectEscrow sdk.AccAddress
}

func newExecuteFixture(t *testing.T, ctx sdk.Context, keepers TestKeepers) executeFixture {
	t.Helper()
	owner := keepers.Faucet.NewFundedAccount(ctx, sdk.NewInt64Coin("denom", 100000))
	_, _, beneficiary := keyPubAddr()

	reflectCode, err := os.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	reflectID, err := keepers.ContractKeeper.Create(ctx, owner, reflectCode, nil)
	require.NoError(t, err)
	reflect, _, err := keepers.ContractKeeper.Instantiate(ctx, reflectID, owner, nil, []byte("{}"), "reflect", nil)
	require.NoError(t, err)

	hackatomCode, err := os.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)
	hackatomID, err := keepers.ContractKeeper.Create(ctx, owner, hackatomCode, nil)
	require.NoError(t, err)
	newEscrow := func(verifier sdk.AccAddress) sdk.AccAddress {
		initMsg, err := json.Marshal(HackatomExampleInitMsg{Verifier: verifier, Beneficiary: beneficiary})
		require.NoError(t, err)
		escrow, _, err := keepers.ContractKeeper.Instantiate(ctx, hackatomID, owner, nil, initMsg, "escrow", sdk.NewCoins(sdk.NewInt64Coin("denom", 1000)))
		require.NoError(t, err)
		return escrow
	}

	return executeFixture{
		owner:         owner,
		beneficiary:   beneficiary,
		escrow:        newEscrow(owner),
		reflect:       reflect,
		reflectEscrow: newEscrow(reflect),
	}
}

// sendDirectly delivers a MsgExecuteContract from owner releasing escrow.
func (f executeFixture) sendDirectly(t *testing.T, ctx sdk.Context, keepers TestKeepers) error {
	t.Helper()
	_, err := NewMsgServerImpl(keepers.WasmKeeper).ExecuteContract(sdk.WrapSDKContext(ctx), &types.MsgExecuteContract{
		Sender:   f.owner.String(),
		Contract: f.escrow.String(),
		Msg:      hackatomRelease,
	})
	return err
}

// dispatchFromContract has the reflect contract dispatch a MsgExecuteContract releasing reflectEscrow.
func (f executeFixture) dispatchFromContract(t *testing.T, ctx sdk.Context, keepers TestKeepers) error {
	t.Helper()
	reflectMsg, err := json.Marshal(ReflectHandleMsg{Reflect: &reflectPayload{Msgs: []wasmvmtypes.CosmosMsg{{
		Wasm: &wasmvmtypes.WasmMsg{Execute: &wasmvmtypes.ExecuteMsg{
			ContractAddr: f.reflectEscrow.String(),
			Msg:          hackatomRelease,
		}},
	}}}})
	require.NoError(t, err)
	_, err = keepers.ContractKeeper.Execute(ctx, f.reflect, f.owner, reflectMsg, nil)
	return err
}

func setMsgExecuteContractDisabled(ctx sdk.Context, keepers TestKeepers, disabled bool) {
	params := types.DefaultParams()
	params.MsgExecuteContractDisabled = disabled
	keepers.WasmKeeper.SetParams(ctx, params)
}

// deleteWasmParam removes key from the wasm param subspace, as in state written before the param existed.
func deleteWasmParam(t *testing.T, ctx sdk.Context, keepers TestKeepers, key []byte) {
	t.Helper()
	for _, storeKey := range keepers.MultiStore.StoreKeys() {
		if storeKey.Name() == paramstypes.StoreKey {
			prefix.NewStore(ctx.KVStore(storeKey), []byte(types.ModuleName+"/")).Delete(key)
			require.False(t, keepers.WasmKeeper.paramSpace.Has(ctx, key))
			return
		}
	}
	t.Fatal("the params store is not mounted")
}

// TestMsgExecuteContractDisabled delivers a MsgExecuteContract directly and through a contract
// that dispatches one, with the msgExecuteContractDisabled param off and on.
func TestMsgExecuteContractDisabled(t *testing.T) {
	specs := map[string]struct {
		disabled       bool
		expBeneficiary sdk.Coins
	}{
		"param off": {disabled: false, expBeneficiary: sdk.NewCoins(sdk.NewInt64Coin("denom", 2000))},
		"param on":  {disabled: true},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			ctx, keepers := CreateTestInput(t, false, ReflectFeatures)
			f := newExecuteFixture(t, ctx, keepers)
			setMsgExecuteContractDisabled(ctx, keepers, spec.disabled)

			for path, deliver := range map[string]func(*testing.T, sdk.Context, TestKeepers) error{
				"sent directly":            f.sendDirectly,
				"dispatched by a contract": f.dispatchFromContract,
			} {
				err := deliver(t, ctx, keepers)
				if spec.disabled {
					require.ErrorIs(t, err, types.ErrMsgExecuteContractDisabled, path)
				} else {
					require.NoError(t, err, path)
				}
			}
			checkAccount(t, ctx, keepers.AccountKeeper, keepers.BankKeeper, f.beneficiary, spec.expBeneficiary)
		})
	}
}

// TestMsgExecuteContractDisabledLeavesKeeperCallsOpen executes a contract through the
// permissioned keeper, the route the EVM wasmd precompile takes, with the param on.
func TestMsgExecuteContractDisabledLeavesKeeperCallsOpen(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures)
	f := newExecuteFixture(t, ctx, keepers)
	setMsgExecuteContractDisabled(ctx, keepers, true)

	_, err := keepers.ContractKeeper.Execute(ctx, f.escrow, f.owner, hackatomRelease, nil)
	require.NoError(t, err)
	checkAccount(t, ctx, keepers.AccountKeeper, keepers.BankKeeper, f.beneficiary, sdk.NewCoins(sdk.NewInt64Coin("denom", 1000)))
}

// TestMsgExecuteContractWithoutTheParam delivers a MsgExecuteContract against state written
// before the msgExecuteContractDisabled param existed.
func TestMsgExecuteContractWithoutTheParam(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures)
	f := newExecuteFixture(t, ctx, keepers)
	deleteWasmParam(t, ctx, keepers, types.ParamStoreKeyMsgExecuteContractDisabled)

	require.NoError(t, f.sendDirectly(t, ctx, keepers))
}

// TestMsgExecuteContractDisabledChargesNoGas reads the msgExecuteContractDisabled param on a
// context that meters param reads.
func TestMsgExecuteContractDisabledChargesNoGas(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, "")
	setMsgExecuteContractDisabled(ctx, keepers, true)
	meter := ctx.GasMeter()

	before := meter.GasConsumed()
	keepers.WasmKeeper.getInstantiateAccessConfig(ctx)
	require.Greater(t, meter.GasConsumed(), before, "the test context does not meter param reads")

	before = meter.GasConsumed()
	require.True(t, keepers.WasmKeeper.msgExecuteContractDisabled(ctx))
	require.Equal(t, before, meter.GasConsumed())
}

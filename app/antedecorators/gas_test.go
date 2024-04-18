package antedecorators_test

import (
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	aclbankmapping "github.com/sei-protocol/sei-chain/aclmapping/bank"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/app/antedecorators/depdecorators"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"github.com/tendermint/tendermint/proto/tendermint/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestMultiplierGasSetter(t *testing.T) {
	testApp := app.Setup(false, false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	ctx := testApp.NewContext(false, types.Header{}).WithBlockHeight(2)
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, *paramtypes.DefaultCosmosGasParams())
	testApp.ParamsKeeper.SetFeesParams(ctx, paramtypes.DefaultGenesis().GetFeesParams())
	testMsg := wasmtypes.MsgExecuteContract{
		Contract: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Msg:      []byte("{\"xyz\":{}}"),
	}
	testTx := app.NewTestTx([]sdk.Msg{&testMsg})

	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: contractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: &accesscontrol.AccessOperation{
					AccessType:         accesscontrol.AccessType_READ,
					ResourceType:       accesscontrol.ResourceType_KV,
					IdentifierTemplate: "something",
				},
			},
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
	})

	// Test with 1/2 cosmos gas multiplier
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, paramtypes.CosmosGasParams{CosmosGasMultiplierNumerator: 1, CosmosGasMultiplierDenominator: 2})
	gasMeterSetter := antedecorators.GetGasMeterSetter(testApp.ParamsKeeper)
	ctxWithGasMeter := gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(1), ctxWithGasMeter.GasMeter().GasConsumed())

	// Test with 1/4 cosmos gas multiplier
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, paramtypes.CosmosGasParams{CosmosGasMultiplierNumerator: 1, CosmosGasMultiplierDenominator: 4})
	ctxWithGasMeter = gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(100, "")
	require.Equal(t, uint64(25), ctxWithGasMeter.GasMeter().GasConsumed())

	// Test over gas limit even with 1/4 gas multiplier
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, paramtypes.CosmosGasParams{CosmosGasMultiplierNumerator: 1, CosmosGasMultiplierDenominator: 4})
	ctxWithGasMeter = gasMeterSetter(false, ctx, 20, testTx)
	require.Panics(t, func() { ctxWithGasMeter.GasMeter().ConsumeGas(100, "") })
	require.Equal(t, true, ctxWithGasMeter.GasMeter().IsOutOfGas())

	// Simulation mode has infinite gas meter with multiplier
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, paramtypes.CosmosGasParams{CosmosGasMultiplierNumerator: 1, CosmosGasMultiplierDenominator: 4})
	// Gas limit is effectively ignored in simulation
	ctxWithGasMeter = gasMeterSetter(true, ctx, 20, testTx)
	require.NotPanics(t, func() { ctxWithGasMeter.GasMeter().ConsumeGas(100, "") })
	require.Equal(t, uint64(25), ctxWithGasMeter.GasMeter().GasConsumed())
	require.Equal(t, false, ctxWithGasMeter.GasMeter().IsOutOfGas())

}

func cacheTxContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}

func TestMultiplierGasMeterOps(t *testing.T) {
	priv1 := secp256k1.GenPrivKey()
	addr1 := sdk.AccAddress(priv1.PubKey().Address())
	priv2 := secp256k1.GenPrivKey()
	addr2 := sdk.AccAddress(priv2.PubKey().Address())
	coins := sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}

	tests := []struct {
		name          string
		expectedError error
		msg           *banktypes.MsgSend
		dynamicDep    bool
	}{
		{
			name:          "default send",
			msg:           banktypes.NewMsgSend(addr1, addr2, coins),
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "dont check synchronous",
			msg:           banktypes.NewMsgSend(addr1, addr2, coins),
			expectedError: nil,
			dynamicDep:    false,
		},
	}

	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	acc2 := &authtypes.BaseAccount{
		Address: addr2.String(),
	}
	accs := authtypes.GenesisAccounts{acc1, acc2}
	balances := []banktypes.Balance{
		{
			Address: addr1.String(),
			Coins:   coins,
		},
		{
			Address: addr2.String(),
			Coins:   coins,
		},
	}

	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	handler := bank.NewHandler(app.BankKeeper)
	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	ctx = ctx.WithMsgValidator(msgValidator)
	anteDecorators := []sdk.AnteFullDecorator{
		sdk.CustomDepWrappedAnteDecorator(ante.NewSetUpContextDecorator(antedecorators.GetGasMeterSetter(app.ParamsKeeper)), depdecorators.GasMeterSetterDecorator{}),
	}
	chainedHandler, depGen := sdk.ChainAnteDecorators(anteDecorators...)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeTx := FakeTx{
				FakeMsgs: []sdk.Msg{
					tc.msg,
				},
			}
			ctx, err := chainedHandler(ctx, fakeTx, false)
			require.Nil(t, err)

			handlerCtx, cms := cacheTxContext(ctx)

			dependencies, _ := aclbankmapping.MsgSendDependencyGenerator(app.AccessControlKeeper, handlerCtx, tc.msg)

			if !tc.dynamicDep {
				dependencies = sdkacltypes.SynchronousAccessOps()
			}

			newDeps, err := depGen(dependencies, fakeTx, 1)
			require.Nil(t, err)

			_, err = handler(handlerCtx, tc.msg)
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}
			missing := handlerCtx.MsgValidator().ValidateAccessOperations(newDeps, cms.GetEvents())
			require.Empty(t, missing)
		})
	}
}

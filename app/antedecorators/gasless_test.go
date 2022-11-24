package antedecorators_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	nitrokeeper "github.com/sei-protocol/sei-chain/x/nitro/keeper"
	nitrotypes "github.com/sei-protocol/sei-chain/x/nitro/types"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

var output = ""
var outputDeps = ""
var gasless = true

type FakeAnteDecoratorOne struct{}

func (ad FakeAnteDecoratorOne) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%sone", output)
	return next(ctx, tx, simulate)
}

func (ad FakeAnteDecoratorOne) AnteDeps(txDeps []accesscontrol.AccessOperation, tx sdk.Tx, next sdk.AnteDepGenerator) (newTxDeps []accesscontrol.AccessOperation, err error) {
	outputDeps = fmt.Sprintf("%sone", outputDeps)
	return next(txDeps, tx)
}

type FakeAnteDecoratorTwo struct{}

func (ad FakeAnteDecoratorTwo) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%stwo", output)
	return next(ctx, tx, simulate)
}

func (ad FakeAnteDecoratorTwo) AnteDeps(txDeps []accesscontrol.AccessOperation, tx sdk.Tx, next sdk.AnteDepGenerator) (newTxDeps []accesscontrol.AccessOperation, err error) {
	outputDeps = fmt.Sprintf("%stwo", outputDeps)
	return next(txDeps, tx)
}

type FakeAnteDecoratorThree struct{}

func (ad FakeAnteDecoratorThree) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%sthree", output)
	return next(ctx, tx, simulate)
}

func (ad FakeAnteDecoratorThree) AnteDeps(txDeps []accesscontrol.AccessOperation, tx sdk.Tx, next sdk.AnteDepGenerator) (newTxDeps []accesscontrol.AccessOperation, err error) {
	outputDeps = fmt.Sprintf("%sthree", outputDeps)
	return next(txDeps, tx)
}

type FakeAnteDecoratorGasReqd struct{}

func (ad FakeAnteDecoratorGasReqd) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	gasless = false
	return next(ctx, tx, simulate)
}

type FakeTx struct {
	sdk.FeeTx
	FakeMsgs []sdk.Msg
}

func (tx FakeTx) GetMsgs() []sdk.Msg {
	return tx.FakeMsgs
}

func (tx FakeTx) ValidateBasic() error {
	return nil
}

func (t FakeTx) GetGas() uint64 {
	return 0
}
func (t FakeTx) GetFee() sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("usei", sdk.ZeroInt()))
}
func (t FakeTx) FeePayer() sdk.AccAddress {
	return nil
}

func (t FakeTx) FeeGranter() sdk.AccAddress {
	return nil
}

func CallGaslessDecoratorWithMsg(ctx sdk.Context, msg sdk.Msg, oracleKeeper oraclekeeper.Keeper, nitroKeeper nitrokeeper.Keeper) error {
	anteDecorators := []sdk.AnteFullDecorator{
		antedecorators.NewGaslessDecorator([]sdk.AnteFullDecorator{sdk.DefaultWrappedAnteDecorator(FakeAnteDecoratorGasReqd{})}, oracleKeeper, nitroKeeper),
	}
	chainedHandler, depGen := sdk.ChainAnteDecorators(anteDecorators...)
	fakeTx := FakeTx{
		FakeMsgs: []sdk.Msg{
			msg,
		},
	}
	_, err := chainedHandler(ctx, fakeTx, false)
	if err != nil {
		return err
	}
	_, err = depGen([]accesscontrol.AccessOperation{}, fakeTx)
	return err
}

func TestGaslessDecorator(t *testing.T) {
	output = ""
	anteDecorators := []sdk.AnteFullDecorator{
		FakeAnteDecoratorOne{},
		antedecorators.NewGaslessDecorator([]sdk.AnteFullDecorator{FakeAnteDecoratorTwo{}}, oraclekeeper.Keeper{}, nitrokeeper.Keeper{}),
		FakeAnteDecoratorThree{},
	}
	chainedHandler, depGen := sdk.ChainAnteDecorators(anteDecorators...)
	_, err := chainedHandler(sdk.Context{}, FakeTx{}, false)
	require.NoError(t, err)
	require.Equal(t, "onetwothree", output)
	_, err = depGen([]accesscontrol.AccessOperation{}, FakeTx{})
	require.NoError(t, err)
	require.Equal(t, "onetwothree", outputDeps)
}

func TestOracleVoteGasless(t *testing.T) {
	input := oraclekeeper.CreateTestInput(t)

	addr := oraclekeeper.Addrs[0]
	addr1 := oraclekeeper.Addrs[1]
	valAddr, val := oraclekeeper.ValAddrs[0], oraclekeeper.ValPubKeys[0]
	valAddr1, val1 := oraclekeeper.ValAddrs[1], oraclekeeper.ValPubKeys[1]
	amt := sdk.TokensFromConsensusPower(100, sdk.DefaultPowerReduction)
	sh := staking.NewHandler(input.StakingKeeper)
	ctx := input.Ctx

	// Validator created
	_, err := sh(ctx, oraclekeeper.NewTestMsgCreateValidator(valAddr, val, amt))
	require.NoError(t, err)
	_, err = sh(ctx, oraclekeeper.NewTestMsgCreateValidator(valAddr1, val1, amt))
	require.NoError(t, err)
	staking.EndBlocker(ctx, input.StakingKeeper)

	input.OracleKeeper.SetAggregateExchangeRateVote(ctx, valAddr, oracletypes.AggregateExchangeRateVote{})

	vote1 := oracletypes.MsgAggregateExchangeRateVote{
		Feeder:    addr.String(),
		Validator: valAddr.String(),
	}

	vote2 := oracletypes.MsgAggregateExchangeRateVote{
		Feeder:    addr1.String(),
		Validator: valAddr1.String(),
	}

	// reset gasless
	gasless = true
	err = CallGaslessDecoratorWithMsg(ctx, &vote1, input.OracleKeeper, nitrokeeper.Keeper{})
	require.NoError(t, err)
	require.False(t, gasless)

	// reset gasless
	gasless = true
	err = CallGaslessDecoratorWithMsg(ctx, &vote2, input.OracleKeeper, nitrokeeper.Keeper{})
	require.NoError(t, err)
	require.True(t, gasless)
}

func TestDexPlaceOrderGasless(t *testing.T) {
	// this needs to be updated if its changed from constant true
	// reset gasless
	gasless = true
	err := CallGaslessDecoratorWithMsg(sdk.Context{}, &types.MsgPlaceOrders{}, oraclekeeper.Keeper{}, nitrokeeper.Keeper{})
	require.NoError(t, err)
	require.True(t, gasless)
}

func TestDexCancelOrderGasless(t *testing.T) {
	addr1 := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	addr2 := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	antedecorators.WhitelistedGaslessCancellationAddrs = []sdk.AccAddress{
		addr2,
	}

	cancelMsg1 := types.MsgCancelOrders{
		Creator: addr1.String(),
	}
	cancelMsg2 := types.MsgCancelOrders{
		Creator: addr2.String(),
	}
	// not whitelisted
	// reset gasless
	gasless = true
	err := CallGaslessDecoratorWithMsg(sdk.Context{}, &cancelMsg1, oraclekeeper.Keeper{}, nitrokeeper.Keeper{})
	require.NoError(t, err)
	require.False(t, gasless)

	// whitelisted
	// reset gasless
	gasless = true
	err = CallGaslessDecoratorWithMsg(sdk.Context{}, &cancelMsg2, oraclekeeper.Keeper{}, nitrokeeper.Keeper{})
	require.NoError(t, err)
	require.True(t, gasless)
}

func TestNitroRecordTxDataGasless(t *testing.T) {
	keeper, ctx := keepertest.NitroKeeper(t)
	// set with non-whitelisted addr
	nonWhitelistedMsg := nitrotypes.MsgRecordTransactionData{
		Sender:    "someone",
		Slot:      1,
		StateRoot: "1234",
		Txs:       []string{"5678"},
	}
	// reset gasless
	gasless = true
	err := CallGaslessDecoratorWithMsg(ctx, &nonWhitelistedMsg, oraclekeeper.Keeper{}, *keeper)
	require.NoError(t, err)
	require.False(t, gasless)

	// set with whitelisted addr
	keeper.SetParams(ctx, nitrotypes.Params{WhitelistedTxSenders: []string{"sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"}})
	whitelistedMsg := nitrotypes.MsgRecordTransactionData{
		Sender:    "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
		Slot:      1,
		StateRoot: "1234",
		Txs:       []string{"5678"},
	}
	// reset gasless
	gasless = true
	err = CallGaslessDecoratorWithMsg(ctx, &whitelistedMsg, oraclekeeper.Keeper{}, *keeper)
	require.NoError(t, err)
	require.True(t, gasless)
}

func TestNonGaslessMsg(t *testing.T) {
	// this needs to be updated if its changed from constant true
	// reset gasless
	gasless = true
	err := CallGaslessDecoratorWithMsg(sdk.Context{}, &types.MsgRegisterContract{}, oraclekeeper.Keeper{}, nitrokeeper.Keeper{})
	require.NoError(t, err)
	require.False(t, gasless)
}

package antedecorators_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var output = ""
var outputDeps = ""
var gasless = true

type FakeAnteDecoratorOne struct{}

func (ad FakeAnteDecoratorOne) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%sone", output)
	return next(ctx, tx, simulate)
}

func (ad FakeAnteDecoratorOne) AnteDeps(txDeps []accesscontrol.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []accesscontrol.AccessOperation, err error) {
	outputDeps = fmt.Sprintf("%sone", outputDeps)
	return next(txDeps, tx, txIndex)
}

type FakeAnteDecoratorTwo struct{}

func (ad FakeAnteDecoratorTwo) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%stwo", output)
	return next(ctx, tx, simulate)
}

func (ad FakeAnteDecoratorTwo) AnteDeps(txDeps []accesscontrol.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []accesscontrol.AccessOperation, err error) {
	outputDeps = fmt.Sprintf("%stwo", outputDeps)
	return next(txDeps, tx, txIndex)
}

type FakeAnteDecoratorThree struct{}

func (ad FakeAnteDecoratorThree) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%sthree", output)
	return next(ctx, tx, simulate)
}

func (ad FakeAnteDecoratorThree) AnteDeps(txDeps []accesscontrol.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []accesscontrol.AccessOperation, err error) {
	outputDeps = fmt.Sprintf("%sthree", outputDeps)
	return next(txDeps, tx, txIndex)
}

type FakeAnteDecoratorGasReqd struct{}

func (ad FakeAnteDecoratorGasReqd) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	gasless = false
	return next(ctx, tx, simulate)
}

type FakeTx struct {
	sdk.FeeTx
	FakeMsgs []sdk.Msg
	Gas      uint64
}

func (tx FakeTx) GetMsgs() []sdk.Msg {
	return tx.FakeMsgs
}

func (tx FakeTx) ValidateBasic() error {
	return nil
}

func (t FakeTx) GetGas() uint64 {
	return t.Gas
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

func CallGaslessDecoratorWithMsg(ctx sdk.Context, msg sdk.Msg, oracleKeeper oraclekeeper.Keeper) error {
	anteDecorators := []sdk.AnteFullDecorator{
		antedecorators.NewGaslessDecorator([]sdk.AnteFullDecorator{sdk.DefaultWrappedAnteDecorator(FakeAnteDecoratorGasReqd{})}, oracleKeeper),
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
	_, err = depGen([]accesscontrol.AccessOperation{}, fakeTx, 1)
	return err
}

func TestOracleVoteGasless(t *testing.T) {
	input := oraclekeeper.CreateTestInput(t)

	addr := oraclekeeper.Addrs[0]
	addr1 := oraclekeeper.Addrs[1]
	valAddr, val := oraclekeeper.ValAddrs[0], oraclekeeper.ValPubKeys[0]
	valAddr1, val1 := oraclekeeper.ValAddrs[1], oraclekeeper.ValPubKeys[1]
	amt := sdk.TokensFromConsensusPower(100, sdk.DefaultPowerReduction)
	sh := staking.NewHandler(input.StakingKeeper)
	ctx := input.Ctx.WithIsCheckTx(true)

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
	err = CallGaslessDecoratorWithMsg(ctx, &vote1, input.OracleKeeper)
	require.Error(t, err)

	// reset gasless
	gasless = true
	err = CallGaslessDecoratorWithMsg(ctx, &vote2, input.OracleKeeper)
	require.NoError(t, err)
	require.True(t, gasless)
}

func TestNonGaslessMsg(t *testing.T) {
	// this needs to be updated if its changed from constant true
	// reset gasless
	gasless = true
	err := CallGaslessDecoratorWithMsg(sdk.NewContext(nil, tmproto.Header{}, false, nil).WithIsCheckTx(true), &oracletypes.MsgDelegateFeedConsent{}, oraclekeeper.Keeper{})
	require.NoError(t, err)
	require.False(t, gasless)
}

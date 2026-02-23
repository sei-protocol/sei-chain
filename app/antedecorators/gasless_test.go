package antedecorators_test

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/app/antedecorators"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletestutils "github.com/sei-protocol/sei-chain/x/oracle/keeper/testutils"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

var output = ""
var outputDeps = ""
var gasless = true

type FakeAnteDecoratorOne struct{}

func (ad FakeAnteDecoratorOne) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%sone", output)
	return next(ctx, tx, simulate)
}

type FakeAnteDecoratorTwo struct{}

func (ad FakeAnteDecoratorTwo) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%stwo", output)
	return next(ctx, tx, simulate)
}

type FakeAnteDecoratorThree struct{}

func (ad FakeAnteDecoratorThree) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	output = fmt.Sprintf("%sthree", output)
	return next(ctx, tx, simulate)
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

func CallGaslessDecoratorWithMsg(ctx sdk.Context, msg sdk.Msg, oracleKeeper oraclekeeper.Keeper, evmKeeper *evmkeeper.Keeper) error {
	anteDecorators := []sdk.AnteDecorator{
		antedecorators.NewGaslessDecorator([]sdk.AnteDecorator{FakeAnteDecoratorGasReqd{}}, oracleKeeper, evmKeeper),
	}
	chainedHandler := sdk.ChainAnteDecorators(anteDecorators...)
	fakeTx := FakeTx{
		FakeMsgs: []sdk.Msg{
			msg,
		},
	}
	_, err := chainedHandler(ctx, fakeTx, false)
	if err != nil {
		return err
	}
	return err
}

func TestOracleVoteGasless(t *testing.T) {
	input := oracletestutils.CreateTestInput(t)

	addr := oracletestutils.Addrs[0]
	addr1 := oracletestutils.Addrs[1]
	valAddr, val := oracletestutils.ValAddrs[0], oracletestutils.ValPubKeys[0]
	valAddr1, val1 := oracletestutils.ValAddrs[1], oracletestutils.ValPubKeys[1]
	amt := sdk.TokensFromConsensusPower(100, sdk.DefaultPowerReduction)
	sh := staking.NewHandler(input.StakingKeeper)
	ctx := input.Ctx.WithIsCheckTx(true)

	// Validator created
	_, err := sh(ctx, oracletestutils.NewTestMsgCreateValidator(valAddr, val, amt))
	require.NoError(t, err)
	_, err = sh(ctx, oracletestutils.NewTestMsgCreateValidator(valAddr1, val1, amt))
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
	err = CallGaslessDecoratorWithMsg(ctx, &vote1, input.OracleKeeper, nil)
	require.Error(t, err)

	// reset gasless
	gasless = true
	err = CallGaslessDecoratorWithMsg(ctx, &vote2, input.OracleKeeper, nil)
	require.NoError(t, err)
	require.True(t, gasless)
}

func TestNonGaslessMsg(t *testing.T) {
	// this needs to be updated if its changed from constant true
	// reset gasless
	gasless = true
	err := CallGaslessDecoratorWithMsg(sdk.NewContext(nil, tmproto.Header{}, false, nil).WithIsCheckTx(true), &oracletypes.MsgDelegateFeedConsent{}, oraclekeeper.Keeper{}, nil)
	require.NoError(t, err)
	require.False(t, gasless)
}

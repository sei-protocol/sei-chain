package antedecorators_test

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/app/antedecorators"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

var output = ""
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

func CallGaslessDecoratorWithMsg(ctx sdk.Context, msg sdk.Msg, evmKeeper *evmkeeper.Keeper) error {
	anteDecorators := []sdk.AnteDecorator{
		antedecorators.NewGaslessDecorator([]sdk.AnteDecorator{FakeAnteDecoratorGasReqd{}}, evmKeeper),
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

func TestOracleMessagesAreNotGasless(t *testing.T) {
	ctx := sdk.NewContext(nil, tmproto.Header{}, false).WithIsCheckTx(true)
	for _, msg := range []sdk.Msg{
		&oracletypes.MsgAggregateExchangeRateVote{},
		&oracletypes.MsgDelegateFeedConsent{},
	} {
		gasless = true
		err := CallGaslessDecoratorWithMsg(ctx, msg, nil)
		require.NoError(t, err)
		require.False(t, gasless)
	}
}

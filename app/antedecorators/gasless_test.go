package antedecorators_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	nitrokeeper "github.com/sei-protocol/sei-chain/x/nitro/keeper"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/stretchr/testify/require"
)

var output = ""

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

type FakeTx struct {
	sdk.FeeTx
}

func (tx FakeTx) GetMsgs() []sdk.Msg {
	return []sdk.Msg{}
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

func TestGaslessDecorator(t *testing.T) {
	output = ""
	anteDecorators := []sdk.AnteFullDecorator{
		sdk.DefaultWrappedAnteDecorator(FakeAnteDecoratorOne{}),
		sdk.DefaultWrappedAnteDecorator(antedecorators.NewGaslessDecorator([]sdk.AnteFullDecorator{sdk.DefaultWrappedAnteDecorator(FakeAnteDecoratorTwo{})}, oraclekeeper.Keeper{}, nitrokeeper.Keeper{})),
		sdk.DefaultWrappedAnteDecorator(FakeAnteDecoratorThree{}),
	}
	chainedHandler, _ := sdk.ChainAnteDecorators(anteDecorators...)
	_, err := chainedHandler(sdk.Context{}, FakeTx{}, false)
	require.NoError(t, err)
	require.Equal(t, "onetwothree", output)
}

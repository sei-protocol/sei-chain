package antedecorators_test

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
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

func TestOnlyUnassociatedAssociateIsGasless(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithIsCheckTx(true)
	sender := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	tx := FakeTx{FakeMsgs: []sdk.Msg{evmtypes.NewMsgAssociate(sender, "test")}}

	isGasless, err := antedecorators.IsTxGasless(tx, ctx, oraclekeeper.Keeper{}, k)
	require.NoError(t, err)
	require.True(t, isGasless)

	k.SetAddressMapping(ctx, sender, common.BytesToAddress(sender))
	isGasless, err = antedecorators.IsTxGasless(tx, ctx, oraclekeeper.Keeper{}, k)
	require.NoError(t, err)
	require.False(t, isGasless)
}

func TestOracleVoteIsNotGasless(t *testing.T) {
	tx := FakeTx{FakeMsgs: []sdk.Msg{&oracletypes.MsgAggregateExchangeRateVote{}}}

	isGasless, err := antedecorators.IsTxGasless(tx, sdk.Context{}, oraclekeeper.Keeper{}, nil)
	require.NoError(t, err)
	require.False(t, isGasless)
}

func TestNonGaslessMsg(t *testing.T) {
	// this needs to be updated if its changed from constant true
	// reset gasless
	gasless = true
	err := CallGaslessDecoratorWithMsg(sdk.NewContext(nil, tmproto.Header{}, false).WithIsCheckTx(true), &oracletypes.MsgDelegateFeedConsent{}, oraclekeeper.Keeper{}, nil)
	require.NoError(t, err)
	require.False(t, gasless)
}

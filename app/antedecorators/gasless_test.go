package antedecorators_test

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletestutils "github.com/sei-protocol/sei-chain/x/oracle/keeper/testutils"
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
	err := CallGaslessDecoratorWithMsg(sdk.NewContext(nil, tmproto.Header{}, false).WithIsCheckTx(true), &oracletypes.MsgDelegateFeedConsent{}, oraclekeeper.Keeper{}, nil)
	require.NoError(t, err)
	require.False(t, gasless)
}

func TestGaslessDecoratorUsesFixedReportedLimitWithoutConsumption(t *testing.T) {
	for _, gasLimit := range []uint64{0, 123_456, 50_000_000} {
		t.Run(fmt.Sprintf("gas_%d", gasLimit), func(t *testing.T) {
			k := &testkeeper.EVMTestApp.EvmKeeper
			ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).
				WithIsCheckTx(true).
				WithGasMeter(sdk.NewGasMeter(gasLimit, 1, 1))
			sender := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
			msg := evmtypes.NewMsgAssociate(sender, "test")
			tx := FakeTx{FakeMsgs: []sdk.Msg{msg}, Gas: gasLimit}
			decorator := antedecorators.NewGaslessDecorator(nil, oraclekeeper.Keeper{}, k)

			resultCtx, err := decorator.AnteHandle(ctx, tx, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
				ctx.GasMeter().ConsumeGas(gasLimit+1, "gasless execution")
				return ctx, nil
			})
			require.NoError(t, err)
			require.Equal(t, antedecorators.FeeExemptTxGasWanted, resultCtx.GasMeter().Limit())
			require.Zero(t, resultCtx.GasMeter().GasConsumed())
			require.Zero(t, resultCtx.GasMeter().GasConsumedToLimit())
			require.False(t, resultCtx.GasMeter().IsPastLimit())
			require.False(t, resultCtx.GasMeter().IsOutOfGas())
			require.NotPanics(t, func() {
				resultCtx.GasMeter().RefundGas(gasLimit+1, "gasless refund")
			})
		})
	}
}

func TestGasWantedForTx(t *testing.T) {
	sender := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	associate := evmtypes.NewMsgAssociate(sender, "test")
	vote := &oracletypes.MsgAggregateExchangeRateVote{}
	consent := &oracletypes.MsgDelegateFeedConsent{}

	testCases := []struct {
		name        string
		tx          FakeTx
		declaredGas uint64
		expectedGas uint64
	}{
		{
			name:        "zero-gas associate uses fixed contribution",
			tx:          FakeTx{FakeMsgs: []sdk.Msg{associate}},
			declaredGas: 0,
			expectedGas: antedecorators.FeeExemptTxGasWanted,
		},
		{
			name:        "high-gas associate uses fixed contribution",
			tx:          FakeTx{FakeMsgs: []sdk.Msg{associate}},
			declaredGas: 50_000_000,
			expectedGas: antedecorators.FeeExemptTxGasWanted,
		},
		{
			name:        "oracle-only transaction uses fixed contribution",
			tx:          FakeTx{FakeMsgs: []sdk.Msg{vote, vote}},
			declaredGas: 50_000_000,
			expectedGas: antedecorators.FeeExemptTxGasWanted,
		},
		{
			name:        "mixed transaction keeps declared contribution",
			tx:          FakeTx{FakeMsgs: []sdk.Msg{associate, vote}},
			declaredGas: 50_000_000,
			expectedGas: 50_000_000,
		},
		{
			name:        "non-exempt oracle message keeps declared contribution",
			tx:          FakeTx{FakeMsgs: []sdk.Msg{consent}},
			declaredGas: 50_000_000,
			expectedGas: 50_000_000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedGas, antedecorators.GasWantedForTx(tc.tx, tc.declaredGas))
		})
	}
}

func TestReportingGasMeterPreservesExecutionLimit(t *testing.T) {
	meter := antedecorators.NewReportingGasMeter(sdk.NewGasMeter(10, 1, 1), 200_000)
	require.Equal(t, uint64(200_000), meter.Limit())

	meter.ConsumeGas(10, "execution limit")
	require.Equal(t, uint64(10), meter.GasConsumed())
	require.True(t, meter.IsOutOfGas())
	require.Panics(t, func() {
		meter.ConsumeGas(1, "past execution limit")
	})
}

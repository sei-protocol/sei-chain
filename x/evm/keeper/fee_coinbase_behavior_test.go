package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

var feeRecipient = common.HexToAddress("0x00000000000000000000000000000000000fee01")

type feeCase struct {
	name                    string
	tipCap                  int64    // gwei
	feeCap                  int64    // gwei
	wantSenderPricePerGas   *big.Int // wei per gas debited from sender
	wantCoinbasePricePerGas *big.Int // wei per gas credited to coinbase
}

// runFeeCase sends a 21000-gas transfer and checks sender, coinbase and surplus accounting.
func runFeeCase(t *testing.T, e *behaviorEnv, nonce uint64, txIndex int, tc feeCase) {
	t.Helper()
	key := mustKey(t, anvilKey0Hex)
	_, sender := keyAddrs(key)
	senderBefore := e.balanceWei(sender)
	coinbaseBefore := e.coinbaseWei(txIndex)
	recipBefore := e.balanceWei(feeRecipient)

	const gasLimit = 50_000
	value := big.NewInt(1) // 1 wei
	r := e.runTx(key, e.dynTx(nonce, &feeRecipient, value, gasLimit, bigGwei(tc.tipCap), bigGwei(tc.feeCap), nil, nil), txIndex)
	require.Empty(t, r.res.VmError)
	require.Equal(t, uint64(21000), r.res.GasUsed)

	senderPaid := new(big.Int).Sub(senderBefore, e.balanceWei(sender))
	coinbaseGot := new(big.Int).Sub(e.coinbaseWei(txIndex), coinbaseBefore)
	require.Equal(t, value, new(big.Int).Sub(e.balanceWei(feeRecipient), recipBefore))

	wantSender := new(big.Int).Add(mulU(r.res.GasUsed, tc.wantSenderPricePerGas), value)
	wantCoinbase := mulU(r.res.GasUsed, tc.wantCoinbasePricePerGas)
	require.Equal(t, wantSender.String(), senderPaid.String(), "sender debit")
	require.Equal(t, wantCoinbase.String(), coinbaseGot.String(), "coinbase credit")

	require.Equal(t, mulU(gasLimit, tc.wantSenderPricePerGas).String(), r.anteSurplus.String(), "ante surplus")
	wantTotalSurplus := new(big.Int).Sub(mulU(r.res.GasUsed, tc.wantSenderPricePerGas), wantCoinbase)
	require.Equal(t, wantTotalSurplus.String(), r.totalSurplus().String(), "total surplus")

	require.Equal(t, tc.wantSenderPricePerGas.Uint64(), r.receipt.EffectiveGasPrice)
}

// g returns n gwei, allowing fractions.
func g(n float64) *big.Int {
	return big.NewInt(int64(n * float64(gwei)))
}

// TestCoinbaseCreditDynamicFeeBehavior pins that the coinbase receives base fee plus tip (no burn).
func TestCoinbaseCreditDynamicFeeBehavior(t *testing.T) {
	e := newBehaviorEnv(t, false)
	e.associateAndFund(mustKey(t, anvilKey0Hex), 1_000_000_000)
	e.k.SetNextBaseFeePerGas(e.ctx, sdk.NewDec(2*gwei))
	require.Equal(t, big.NewInt(2*gwei), e.k.GetBaseFee(e.ctx))

	// feeCap > base+tip: pays base+tip
	runFeeCase(t, e, 0, 0, feeCase{name: "cap_above", tipCap: 1, feeCap: 10, wantSenderPricePerGas: g(3), wantCoinbasePricePerGas: g(3)})
	// feeCap < base+tip: pays feeCap
	tc := feeCase{name: "cap_below", tipCap: 1, wantSenderPricePerGas: g(2.5), wantCoinbasePricePerGas: g(2.5)}
	key := mustKey(t, anvilKey0Hex)
	_, sender := keyAddrs(key)
	senderBefore := e.balanceWei(sender)
	cbBefore := e.coinbaseWei(1)
	r := e.runTx(key, e.dynTx(1, &feeRecipient, nil, 50_000, bigGwei(tc.tipCap), g(2.5), nil, nil), 1)
	require.Empty(t, r.res.VmError)
	require.Equal(t, uint64(21000), r.res.GasUsed)
	require.Equal(t, big.NewInt(52_500_000_000_000), new(big.Int).Sub(senderBefore, e.balanceWei(sender))) // 21000*2.5gwei
	require.Equal(t, big.NewInt(52_500_000_000_000), new(big.Int).Sub(e.coinbaseWei(1), cbBefore))
	require.True(t, r.totalSurplus().IsZero())
	// tip == 0: coinbase still receives base fee
	runFeeCase(t, e, 2, 2, feeCase{name: "zero_tip", tipCap: 0, feeCap: 10, wantSenderPricePerGas: g(2), wantCoinbasePricePerGas: g(2)})
}

// pacific-1 fee gating constants.
const (
	pacificNextBaseFeeHeight = int64(114945913) // from here block ctx uses next base fee
	upgrade620               = "6.2.0"          // before this, GetBaseFee returns nil
)

func newPacificEnv(t *testing.T, height int64, done620 int64, paramBaseFeeGwei int64, nextBaseFeeGwei int64) *behaviorEnv {
	e := newBehaviorEnv(t, false)
	e.ctx = e.ctx.WithChainID(keeper.Pacific1ChainID).WithBlockHeight(height)
	require.Equal(t, int64(1329), e.k.ChainID(e.ctx).Int64())
	if done620 > 0 {
		e.app.UpgradeKeeper.SetDone(e.ctx.WithBlockHeight(done620), upgrade620)
	}
	p := e.k.GetParams(e.ctx)
	p.BaseFeePerGas = sdk.NewDec(paramBaseFeeGwei * gwei)
	e.k.SetParams(e.ctx, p)
	e.k.SetNextBaseFeePerGas(e.ctx, sdk.NewDec(nextBaseFeeGwei*gwei))
	e.associateAndFund(mustKey(t, anvilKey0Hex), 1_000_000_000)
	return e
}

// TestCoinbaseCreditPacificLegacyBehavior pins pacific-1 fee paths where sender price and coinbase credit differ.
func TestCoinbaseCreditPacificLegacyBehavior(t *testing.T) {
	t.Run("below_114945913_and_below_v6.2.0", func(t *testing.T) {
		// sender pays feeCap; block ctx base fee = param
		e := newPacificEnv(t, 100_000_000, 120_000_000, 1, 2)
		require.Nil(t, e.k.GetBaseFee(e.ctx))
		runFeeCase(t, e, 0, 0, feeCase{tipCap: 1, feeCap: 10, wantSenderPricePerGas: g(10), wantCoinbasePricePerGas: g(2)})
		// feeCap < ctxBase+tip: both pay feeCap
		runFeeCase2(t, e, 1, 1, 1, 1.5, g(1.5), g(1.5))
	})
	t.Run("at_or_above_114945913_below_v6.2.0", func(t *testing.T) {
		// sender pays feeCap; block ctx base fee = next base fee
		e := newPacificEnv(t, pacificNextBaseFeeHeight, 120_000_000, 1, 2)
		require.Nil(t, e.k.GetBaseFee(e.ctx))
		runFeeCase(t, e, 0, 0, feeCase{tipCap: 1, feeCap: 10, wantSenderPricePerGas: g(10), wantCoinbasePricePerGas: g(3)})
	})
	t.Run("below_114945913_at_or_above_v6.2.0", func(t *testing.T) {
		// coinbase credited more than sender paid: negative surplus
		e := newPacificEnv(t, 100_000_000, 50_000_000, 5, 2)
		require.Equal(t, big.NewInt(2*gwei), e.k.GetBaseFee(e.ctx))
		runFeeCase(t, e, 0, 0, feeCase{tipCap: 1, feeCap: 10, wantSenderPricePerGas: g(3), wantCoinbasePricePerGas: g(6)})
	})
	t.Run("pacific_after_both_gates", func(t *testing.T) {
		// same as non-pacific path
		e := newPacificEnv(t, 130_000_000, 120_000_000, 5, 2)
		runFeeCase(t, e, 0, 0, feeCase{tipCap: 1, feeCap: 10, wantSenderPricePerGas: g(3), wantCoinbasePricePerGas: g(3)})
	})
}

func runFeeCase2(t *testing.T, e *behaviorEnv, nonce uint64, txIndex int, tip int64, feeCap float64, sender, coinbase *big.Int) {
	t.Helper()
	key := mustKey(t, anvilKey0Hex)
	_, s := keyAddrs(key)
	sBefore := e.balanceWei(s)
	cBefore := e.coinbaseWei(txIndex)
	r := e.runTx(key, e.dynTx(nonce, &feeRecipient, nil, 50_000, bigGwei(tip), g(feeCap), nil, nil), txIndex)
	require.Empty(t, r.res.VmError)
	require.Equal(t, mulU(r.res.GasUsed, sender).String(), new(big.Int).Sub(sBefore, e.balanceWei(s)).String())
	require.Equal(t, mulU(r.res.GasUsed, coinbase).String(), new(big.Int).Sub(e.coinbaseWei(txIndex), cBefore).String())
}

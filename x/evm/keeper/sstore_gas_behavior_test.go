package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// EIP-2929/2200/3529 gas constants.
const (
	txGas          = uint64(21000)
	pushGas        = uint64(3)
	coldSloadGas   = uint64(2100)
	warmReadGas    = uint64(100)
	refundQuotient = uint64(5)
)

var (
	sstoreSetContract   = common.HexToAddress("0x00000000000000000000000000000000005e7001")
	sstoreResetContract = common.HexToAddress("0x00000000000000000000000000000000005e7002")
)

// runtime: SSTORE(0, 0x2a); STOP
func sstoreSetCode() []byte {
	return (&bc{}).push1(0x2a).push1(0).op(vm.SSTORE, vm.STOP).bytes()
}

// runtime: SSTORE(0, 0x2a); SSTORE(0, 0); STOP
func sstoreSetResetCode() []byte {
	return (&bc{}).push1(0x2a).push1(0).op(vm.SSTORE).push1(0).push1(0).op(vm.SSTORE, vm.STOP).bytes()
}

// expectedSetGas: intrinsic + 2 PUSH1 + cold slot + SeiSstoreSetGasEIP2200.
func expectedSetGas(param uint64) uint64 {
	return txGas + 2*pushGas + coldSloadGas + param
}

// expectedSetResetGas: 0->A->0 gas and refund (param - 100, capped at 1/5).
func expectedSetResetGas(param uint64) (gasUsed uint64, refund uint64) {
	pre := txGas + 4*pushGas + coldSloadGas + param + warmReadGas
	refund = param - warmReadGas
	if limit := pre / refundQuotient; refund > limit {
		refund = limit
	}
	return pre - refund, refund
}

func setSstoreParam(e *behaviorEnv, v uint64) {
	p := e.k.GetParams(e.ctx)
	p.SeiSstoreSetGasEip2200 = v
	e.k.SetParams(e.ctx, p)
}

func runSstoreTx(t *testing.T, e *behaviorEnv, to common.Address, nonce uint64, txIndex int) uint64 {
	key := mustKey(t, anvilKey0Hex)
	r := e.runTx(key, e.legacyTx(nonce, &to, nil, 200_000, bigGwei(1), nil), txIndex)
	require.Empty(t, r.res.VmError)
	require.Equal(t, r.res.GasUsed, r.receipt.GasUsed)
	return r.res.GasUsed
}

// TestSstoreSetGasParamBehavior pins SSTORE set cost and 0->A->0 refund for several param values.
func TestSstoreSetGasParamBehavior(t *testing.T) {
	for _, tc := range []struct {
		name           string
		param          uint64 // value written to the param store
		rawStore       bool   // bypass SetParams validation
		effective      uint64 // expected getter value
		wantSet        uint64
		wantSetReset   uint64
		wantRefundUsed uint64
	}{
		// set = 21000 + 6 push + 2100 cold + param
		{name: "default_20000", param: 20000, effective: 20000, wantSet: 43106, wantSetReset: 34570, wantRefundUsed: 8642},
		{name: "param_72000", param: 72000, effective: 72000, wantSet: 95106, wantSetReset: 76170, wantRefundUsed: 19042},
		// refund below 1/5 cap
		{name: "param_5000_uncapped_refund", param: 5000, effective: 5000, wantSet: 28106, wantSetReset: 23312, wantRefundUsed: 4900},
		// zero falls back to legacy 20000
		{name: "param_zero_legacy_fallback", param: 0, rawStore: true, effective: keeper.LegacySstoreSetGasEIP2200, wantSet: 43106, wantSetReset: 34570, wantRefundUsed: 8642},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newBehaviorEnv(t, false)
			e.associateAndFund(mustKey(t, anvilKey0Hex), 1_000_000)
			e.k.SetCode(e.ctx, sstoreSetContract, sstoreSetCode())
			e.k.SetCode(e.ctx, sstoreResetContract, sstoreSetResetCode())
			if tc.rawStore {
				e.k.Paramstore.Set(e.ctx, types.KeySeiSstoreSetGasEIP2200, tc.param)
				require.Equal(t, tc.param, e.k.GetParams(e.ctx).SeiSstoreSetGasEip2200)
			} else {
				setSstoreParam(e, tc.param)
			}
			require.Equal(t, tc.effective, e.k.GetSstoreSetGasEIP2200(e.ctx))

			require.Equal(t, tc.wantSet, expectedSetGas(tc.effective))
			gu, refund := expectedSetResetGas(tc.effective)
			require.Equal(t, tc.wantSetReset, gu)
			require.Equal(t, tc.wantRefundUsed, refund)

			require.Equal(t, tc.wantSet, runSstoreTx(t, e, sstoreSetContract, 0, 0))
			require.Equal(t, big.NewInt(0x2a), e.slot(sstoreSetContract, 0))

			require.Equal(t, tc.wantSetReset, runSstoreTx(t, e, sstoreResetContract, 1, 1))
			require.Equal(t, int64(0), e.slot(sstoreResetContract, 0).Int64())

			// repeat SSTORE of same value: no-op write
			require.Equal(t, txGas+2*pushGas+coldSloadGas+warmReadGas, runSstoreTx(t, e, sstoreSetContract, 2, 2))
		})
	}
}

// TestSstoreSetGasGetterBehavior pins GetSstoreSetGasEIP2200: stored value, or legacy 20000 when zero.
func TestSstoreSetGasGetterBehavior(t *testing.T) {
	e := newBehaviorEnv(t, false)
	require.Equal(t, uint64(20000), types.DefaultSeiSstoreSetGasEIP2200)
	require.Equal(t, uint64(20000), e.k.GetSstoreSetGasEIP2200(e.ctx))
	setSstoreParam(e, 72000)
	require.Equal(t, uint64(72000), e.k.GetSstoreSetGasEIP2200(e.ctx))
	// Validate rejects 0
	p := e.k.GetParams(e.ctx)
	p.SeiSstoreSetGasEip2200 = 0
	require.Error(t, p.Validate())
	// stored zero falls back to 20000
	e.k.Paramstore.Set(e.ctx, types.KeySeiSstoreSetGasEIP2200, uint64(0))
	require.Equal(t, uint64(20000), e.k.GetSstoreSetGasEIP2200(e.ctx))
	require.Equal(t, uint64(20000), keeper.LegacySstoreSetGasEIP2200)
}

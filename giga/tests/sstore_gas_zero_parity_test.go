package giga_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	gigatypes "github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// CON-510: Giga's newGigaBlockCache (app/app.go) used to read
// SeiSstoreSetGasEip2200 straight off the stored param, with no fallback for a
// stored/absent 0 (the value a migration materializes, or state from before
// the param existed). V2 always goes through
// keeper.GetSstoreSetGasEIP2200, which maps 0 -> LegacySstoreSetGasEIP2200
// (20000). These tests force the param to a raw 0 (bypassing SetParams
// validation, which rejects 0) and assert Giga agrees with V2 at every tier:
// gas used, full receipt, and the consensus-critical LastResultsHash.

// ---- tiny bytecode assembler (same technique as
// x/evm/keeper/sstore_gas_behavior_test.go on shemnon/evm-geth-behavior-tests) ----

type sstoreBC struct{ code []byte }

func (b *sstoreBC) op(ops ...vm.OpCode) *sstoreBC {
	for _, o := range ops {
		b.code = append(b.code, byte(o))
	}
	return b
}

func (b *sstoreBC) push1(v byte) *sstoreBC { b.code = append(b.code, byte(vm.PUSH1), v); return b }
func (b *sstoreBC) bytes() []byte          { return b.code }
func (b *sstoreBC) pc() int                { return len(b.code) }

// sstoreInitcode wraps runtime code in a trivial constructor that just
// returns it: PUSH1 len DUP1 PUSH1 off PUSH1 0 CODECOPY PUSH1 0 RETURN.
func sstoreInitcode(runtime []byte) []byte {
	const off = 11
	if len(runtime) > 255 {
		panic("sstoreInitcode: runtime too large for PUSH1 encoding")
	}
	pre := (&sstoreBC{}).push1(byte(len(runtime))).op(vm.DUP1).push1(off).push1(0).op(vm.CODECOPY).push1(0).op(vm.RETURN)
	return append(pre.bytes(), runtime...)
}

// setSstoreParamRaw writes the SSTORE gas param directly to tCtx's keeper's
// param store, bypassing SetParams validation. This mirrors how the storage
// migration path materializes an absent param as an explicit stored 0 (see
// x/evm/migrations/migrate_sstore_gas.go and the #2667 zero fallback).
func setSstoreParamRaw(tCtx *GigaTestContext, v uint64) {
	if tCtx.isGigaMode() {
		tCtx.TestApp.GigaEvmKeeper.Paramstore.Set(tCtx.Ctx, gigatypes.KeySeiSstoreSetGasEIP2200, v)
	} else {
		tCtx.TestApp.EvmKeeper.Paramstore.Set(tCtx.Ctx, evmtypes.KeySeiSstoreSetGasEIP2200, v)
	}
}

// setStateRaw writes a contract storage slot directly via the keeper (no EVM
// execution, no gas), so a test can establish a slot's pre-tx ("original")
// value without that write itself costing param-dependent SSTORE gas.
func setStateRaw(tCtx *GigaTestContext, addr common.Address, slot, val common.Hash) {
	if tCtx.isGigaMode() {
		tCtx.TestApp.GigaEvmKeeper.SetState(tCtx.Ctx, addr, slot, val)
	} else {
		tCtx.TestApp.EvmKeeper.SetState(tCtx.Ctx, addr, slot, val)
	}
}

// deployAndCall deploys initcode (signed by deployer, nonce 0) and calls the
// deployed contract once with no calldata (nonce 1), both in the same block.
// Returns [deployResult, callResult]. The caller supplies deployer so every
// mode signs byte-identical transactions: receipts are only comparable across
// contexts when their tx hashes match.
func deployAndCall(t testing.TB, tCtx *GigaTestContext, deployer utils.TestAcct, initcode []byte) []*abci.ExecTxResult {
	isGiga := tCtx.isGigaMode()
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)

	deployTxs := CreateContractDeployTxs(t, tCtx, []EVMContractDeploy{
		{Signer: deployer, Bytecode: initcode, Nonce: 0},
	}, isGiga)
	callTxs := CreateContractCallTxs(t, tCtx, []EVMContractCall{
		{Signer: deployer, Contract: contractAddr, Nonce: 1},
	}, isGiga)

	_, results, err := RunBlock(t, tCtx, append(deployTxs, callTxs...))
	require.NoError(t, err)
	require.Len(t, results, 2)
	return results
}

// runSstoreZeroScenario builds a fresh test context for mode with the SSTORE
// gas param forced to a raw 0, then deploys and calls initcode as deployer.
func runSstoreZeroScenario(t *testing.T, mode ExecutorMode, workers int, deployer utils.TestAcct, initcode []byte) (*GigaTestContext, []*abci.ExecTxResult) {
	accts := utils.NewTestAccounts(3)
	tCtx := NewGigaTestContext(t, accts, time.Now(), workers, mode)
	setSstoreParamRaw(tCtx, 0)
	return tCtx, deployAndCall(t, tCtx, deployer, initcode)
}

// assertZeroParamParity runs initcode under V2, GigaSequential, and GigaOCC
// with the SSTORE param forced to a raw 0, and checks every consensus-visible
// field agrees across all three: result codes, gas used, full receipts, and
// the LastResultsHash that actually drives block agreement.
func assertZeroParamParity(t *testing.T, testName string, initcode []byte) (v2Results, seqResults, occResults []*abci.ExecTxResult) {
	deployer := utils.NewSigner()
	v2Ctx, v2Results := runSstoreZeroScenario(t, ModeV2withOCC, 1, deployer, initcode)
	seqCtx, seqResults := runSstoreZeroScenario(t, ModeGigaSequential, 1, deployer, initcode)
	occCtx, occResults := runSstoreZeroScenario(t, ModeGigaOCC, 4, deployer, initcode)

	CompareResults(t, testName+"_V2VsGigaSeq", v2Results, seqResults)
	CompareReceipts(t, testName+"_V2VsGigaSeq", v2Ctx, v2Results, seqCtx, seqResults)
	CompareLastResultsHash(t, testName+"_V2VsGigaSeq", v2Results, seqResults)

	CompareResults(t, testName+"_GigaSeqVsGigaOCC", seqResults, occResults)
	CompareReceipts(t, testName+"_GigaSeqVsGigaOCC", seqCtx, seqResults, occCtx, occResults)
	CompareLastResultsHash(t, testName+"_GigaSeqVsGigaOCC", seqResults, occResults)

	return v2Results, seqResults, occResults
}

// TestSstoreGasZeroParity_CleanSet: a single clean SSTORE (0 -> non-zero) on a
// fresh slot. With the param read raw, Giga charges only the cold-access cost
// (2100) instead of cold + SeiSstoreSetGasEip2200 (param-dependent, 20000 by
// default) -- roughly 10x-35x cheaper storage writes, a state-growth/spam risk.
func TestSstoreGasZeroParity_CleanSet(t *testing.T) {
	runtime := (&sstoreBC{}).push1(0x2a).push1(0).op(vm.SSTORE, vm.STOP).bytes()
	assertZeroParamParity(t, "SstoreGasZeroParity_CleanSet", sstoreInitcode(runtime))
}

// TestSstoreGasZeroParity_SetThenReset: 0 -> A -> 0 on a fresh slot in a single
// tx. The dirty-slot-restored-to-original refund is
// AddRefund(param - WarmStorageReadCostEIP2929); with param read raw as 0 this
// is AddRefund(0 - 100), a uint64 underflow. Both Sei StateDBs' AddRefund add
// without an overflow check, so this corrupts the refund counter instead of
// erroring.
func TestSstoreGasZeroParity_SetThenReset(t *testing.T) {
	runtime := (&sstoreBC{}).
		push1(0x2a).push1(0).op(vm.SSTORE). // SSTORE(slot0, 0x2a)
		push1(0).push1(0).op(vm.SSTORE).    // SSTORE(slot0, 0) - back to original
		op(vm.STOP).
		bytes()
	assertZeroParamParity(t, "SstoreGasZeroParity_SetThenReset", sstoreInitcode(runtime))
}

// TestSstoreGasZeroParity_RefundUnderflowPanic exercises the corrupted refund
// counter from the 0 -> A -> 0 underflow (see TestSstoreGasZeroParity_SetThenReset)
// until it goes negative:
//  1. SSTORE(slot0, 0): clear a slot that was non-zero at the start of this
//     tx -> AddRefund(SSTORE_CLEARS_SCHEDULE=4800).
//  2. SSTORE(slot1, A); SSTORE(slot1, 0): 0 -> A -> 0 on a fresh slot ->
//     AddRefund(param - 100); with param==0 this wraps and (since the counter
//     is already >= 100) nets out to "counter -= 100".
//  3. SSTORE(slot0, A): recreate the slot cleared in step 1 ->
//     SubRefund(SSTORE_CLEARS_SCHEDULE=4800). The counter from steps 1-2 is
//     now below 4800, so this underflows and panics
//     ("Refund counter below zero") on the buggy (raw-param) path.
//
// slot0's pre-tx value is seeded via a direct, gas-free keeper write (not a
// constructor SSTORE) so this test isolates the one tx that matters: the
// deploy tx is plain (no SSTORE) and must already have identical gas/receipts
// across all three paths, leaving the call tx's pass/fail as the only
// meaningful divergence to assert on.
//
// Giga's panic-recovery middleware turns that panic into a failed tx, so
// pre-fix this tx SUCCEEDS on V2 (fallback keeps the counter comfortably
// positive) and FAILS on Giga: an on-demand V2/Giga consensus divergence, not
// merely a gas mismatch.
func TestSstoreGasZeroParity_RefundUnderflowPanic(t *testing.T) {
	runtime := (&sstoreBC{}).
		push1(0).push1(0).op(vm.SSTORE).    // SSTORE(slot0, 0): clear
		push1(0x2a).push1(1).op(vm.SSTORE). // SSTORE(slot1, 0x2a): fresh set
		push1(0).push1(1).op(vm.SSTORE).    // SSTORE(slot1, 0): back to original
		push1(0x2a).push1(0).op(vm.SSTORE). // SSTORE(slot0, 0x2a): recreate cleared slot
		op(vm.STOP).
		bytes()
	initcode := sstoreInitcode(runtime)

	deployer := utils.NewSigner()
	contractAddr := crypto.CreateAddress(deployer.EvmAddress, 0)
	slot0 := common.BigToHash(big.NewInt(0))
	nonzero := common.BigToHash(big.NewInt(1))

	run := func(mode ExecutorMode, workers int) (*GigaTestContext, []*abci.ExecTxResult) {
		accts := utils.NewTestAccounts(3)
		tCtx := NewGigaTestContext(t, accts, time.Now(), workers, mode)
		setSstoreParamRaw(tCtx, 0)
		setStateRaw(tCtx, contractAddr, slot0, nonzero) // slot0's "original" for the call tx below
		return tCtx, deployAndCall(t, tCtx, deployer, initcode)
	}

	v2Ctx, v2Results := run(ModeV2withOCC, 1)
	seqCtx, seqResults := run(ModeGigaSequential, 1)
	occCtx, occResults := run(ModeGigaOCC, 4)

	const testName = "SstoreGasZeroParity_RefundUnderflowPanic"
	CompareResults(t, testName+"_V2VsGigaSeq", v2Results, seqResults)
	CompareReceipts(t, testName+"_V2VsGigaSeq", v2Ctx, v2Results, seqCtx, seqResults)
	CompareLastResultsHash(t, testName+"_V2VsGigaSeq", v2Results, seqResults)

	CompareResults(t, testName+"_GigaSeqVsGigaOCC", seqResults, occResults)
	CompareReceipts(t, testName+"_GigaSeqVsGigaOCC", seqCtx, seqResults, occCtx, occResults)
	CompareLastResultsHash(t, testName+"_GigaSeqVsGigaOCC", seqResults, occResults)

	// The specific regression this guards: all three paths must agree the call
	// tx SUCCEEDS, not merely agree with each other on a shared failure code.
	require.Equal(t, uint32(0), v2Results[1].Code, "V2 call should succeed: %s", v2Results[1].Log)
	require.Equal(t, uint32(0), seqResults[1].Code, "GigaSequential call should succeed: %s", seqResults[1].Log)
	require.Equal(t, uint32(0), occResults[1].Code, "GigaOCC call should succeed: %s", occResults[1].Log)
}

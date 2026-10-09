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

// CON-510: forces the SSTORE gas param to a raw 0 (bypassing SetParams
// validation) and checks Giga agrees with V2 on gas, receipts, and LastResultsHash.

// ---- minimal bytecode assembler for SSTORE test contracts ----

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

// sstoreInitcode wraps runtime in a trivial constructor that just returns it.
func sstoreInitcode(runtime []byte) []byte {
	const off = 11
	if len(runtime) > 255 {
		panic("sstoreInitcode: runtime too large for PUSH1 encoding")
	}
	pre := (&sstoreBC{}).push1(byte(len(runtime))).op(vm.DUP1).push1(off).push1(0).op(vm.CODECOPY).push1(0).op(vm.RETURN)
	return append(pre.bytes(), runtime...)
}

// setSstoreParamRaw writes the SSTORE gas param directly, bypassing
// SetParams validation (which rejects 0).
func setSstoreParamRaw(tCtx *GigaTestContext, v uint64) {
	if tCtx.isGigaMode() {
		tCtx.TestApp.GigaEvmKeeper.Paramstore.Set(tCtx.Ctx, gigatypes.KeySeiSstoreSetGasEIP2200, v)
	} else {
		tCtx.TestApp.EvmKeeper.Paramstore.Set(tCtx.Ctx, evmtypes.KeySeiSstoreSetGasEIP2200, v)
	}
}

// setStateRaw writes a contract storage slot directly via the keeper, with
// no EVM execution and no gas cost.
func setStateRaw(tCtx *GigaTestContext, addr common.Address, slot, val common.Hash) {
	if tCtx.isGigaMode() {
		tCtx.TestApp.GigaEvmKeeper.SetState(tCtx.Ctx, addr, slot, val)
	} else {
		tCtx.TestApp.EvmKeeper.SetState(tCtx.Ctx, addr, slot, val)
	}
}

// deployAndCall deploys initcode and calls it once with no calldata, both in
// one block. deployer must be shared across modes so receipts stay comparable.
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

// runSstoreZeroScenario builds a fresh context for mode with the SSTORE param
// forced to 0, then deploys and calls initcode as deployer.
func runSstoreZeroScenario(t *testing.T, mode ExecutorMode, workers int, deployer utils.TestAcct, initcode []byte) (*GigaTestContext, []*abci.ExecTxResult) {
	accts := utils.NewTestAccounts(3)
	tCtx := NewGigaTestContext(t, accts, time.Now(), workers, mode)
	setSstoreParamRaw(tCtx, 0)
	return tCtx, deployAndCall(t, tCtx, deployer, initcode)
}

// assertZeroParamParity checks V2, GigaSequential, and GigaOCC agree (code,
// gas, receipts, LastResultsHash) with the SSTORE param forced to 0.
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

// A clean 0 -> non-zero SSTORE: Giga undercharges when the param reads as 0.
func TestSstoreGasZeroParity_CleanSet(t *testing.T) {
	runtime := (&sstoreBC{}).push1(0x2a).push1(0).op(vm.SSTORE, vm.STOP).bytes()
	assertZeroParamParity(t, "SstoreGasZeroParity_CleanSet", sstoreInitcode(runtime))
}

// 0 -> A -> 0 in one tx: the refund math (AddRefund(param - 100)) underflows
// when the param reads as 0, corrupting the refund counter instead of erroring.
func TestSstoreGasZeroParity_SetThenReset(t *testing.T) {
	runtime := (&sstoreBC{}).
		push1(0x2a).push1(0).op(vm.SSTORE). // SSTORE(slot0, 0x2a)
		push1(0).push1(0).op(vm.SSTORE).    // SSTORE(slot0, 0) - back to original
		op(vm.STOP).
		bytes()
	assertZeroParamParity(t, "SstoreGasZeroParity_SetThenReset", sstoreInitcode(runtime))
}

// Drives the corrupted refund counter negative via clear/reset/recreate.
// Pre-fix this panics on Giga (recovered as a failed tx) while V2 succeeds.
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

	// All three must agree the call tx SUCCEEDS, not just agree with each other.
	require.Equal(t, uint32(0), v2Results[1].Code, "V2 call should succeed: %s", v2Results[1].Log)
	require.Equal(t, uint32(0), seqResults[1].Code, "GigaSequential call should succeed: %s", seqResults[1].Log)
	require.Equal(t, uint32(0), occResults[1].Code, "GigaOCC call should succeed: %s", occResults[1].Log)
}

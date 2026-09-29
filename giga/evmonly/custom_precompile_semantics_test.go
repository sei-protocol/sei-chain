package evmonly

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"slices"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/program"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	probeAddr      = common.HexToAddress("0x0000000000000000000000000000000000001003")
	unresolvedAddr = common.HexToAddress("0x0000000000000000000000000000000000001004")
	probeSlot      = common.Hash{0x51}
	errProbeFailed = errors.New("probe failed")

	proxySuccessSlot    = common.Hash{0x61}
	proxyReturnSizeSlot = common.Hash{0x62}
	proxyReturnSlot     = common.Hash{0x63}
)

// oneByteTxIntrinsicGas is the intrinsic gas of a call carrying one non-zero data byte.
const oneByteTxIntrinsicGas = 21_000 + 16

// scriptedContract is a custom precompile whose gas and behavior the test supplies.
type scriptedContract struct {
	gas func([]byte) uint64
	run func(*precompiles.Context, []byte) ([]byte, error)
}

func (c scriptedContract) RequiredGas(input []byte) uint64 {
	if c.gas == nil {
		return 0
	}
	return c.gas(input)
}

func (c scriptedContract) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	return c.run(ctx, input)
}

func fixedGas(gas uint64) func([]byte) uint64 {
	return func([]byte) uint64 { return gas }
}

// registryOf resolves each address to its contract. An address mapped to nil
// is listed but unresolved.
type registryOf map[common.Address]precompiles.Contract

func (r registryOf) Get(addr common.Address) (precompiles.Contract, bool) {
	contract, ok := r[addr]
	return contract, ok && contract != nil
}

func (r registryOf) Addresses() []common.Address {
	addrs := make([]common.Address, 0, len(r))
	for addr := range r {
		addrs = append(addrs, addr)
	}
	slices.SortFunc(addrs, func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) })
	return addrs
}

// contextRecord is the part of a precompiles.Context that a call observed.
type contextRecord struct {
	Caller        common.Address
	Address       common.Address
	ApparentValue *big.Int
	ReadOnly      bool
	DelegateCall  bool
	GasRemaining  uint64
	Block         precompiles.BlockContext
}

// contextRecorder is a custom precompile that records the context of every call.
type contextRecorder struct {
	mu      sync.Mutex
	records []contextRecord
}

func (r *contextRecorder) contract(gas uint64) scriptedContract {
	return scriptedContract{gas: fixedGas(gas), run: func(ctx *precompiles.Context, _ []byte) ([]byte, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.records = append(r.records, contextRecord{
			Caller:        ctx.Caller,
			Address:       ctx.Address,
			ApparentValue: ctx.ApparentValue,
			ReadOnly:      ctx.ReadOnly,
			DelegateCall:  ctx.DelegateCall,
			GasRemaining:  ctx.GasRemaining,
			Block:         ctx.Block,
		})
		return nil, nil
	}}
}

func (r *contextRecorder) only(t *testing.T) contextRecord {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.records, 1)
	return r.records[0]
}

func (r *contextRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

type testAccount struct {
	key  *ecdsa.PrivateKey
	addr common.Address
}

func newTestAccount(t *testing.T, states ...*MemoryState) testAccount {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	account := testAccount{key: key, addr: crypto.PubkeyToAddress(key.PublicKey)}
	for _, state := range states {
		state.SetBalance(account.addr, big.NewInt(testFundedBalanceWei))
	}
	return account
}

func callTx(t *testing.T, from testAccount, nonce uint64, to common.Address, value int64, data []byte, gas uint64) []byte {
	t.Helper()
	return signLegacyTxWithGasPrice(t, from.key, big.NewInt(testChainID), nonce, &to, big.NewInt(value), data, gas, big.NewInt(0))
}

// runPrecompileBlock executes rawTxs sequentially against state and applies the result.
func runPrecompileBlock(t *testing.T, state *MemoryState, registry precompiles.Registry, rawTxs ...[]byte) *BlockResult {
	t.Helper()
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: registry}, withTestState(state))
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID)), Txs: rawTxs})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	return result
}

// precompileProxyRuntime returns runtime bytecode that forwards its calldata to
// target with op, sending value for CALL and CALLCODE. It stores the call's
// success flag, return data size, and first return word, then stops or reverts.
func precompileProxyRuntime(op vm.OpCode, target common.Address, value uint64, revertAfter bool) []byte {
	p := program.New()
	p.Op(vm.CALLDATASIZE).Push(0).Push(0).Op(vm.CALLDATACOPY)
	p.Push(32).Push(0).Op(vm.CALLDATASIZE).Push(0)
	if op == vm.CALL || op == vm.CALLCODE {
		p.Push(value)
	}
	p.Push(target).Op(vm.GAS, op)
	p.Push(proxySuccessSlot).Op(vm.SSTORE)
	p.Op(vm.RETURNDATASIZE).Push(proxyReturnSizeSlot).Op(vm.SSTORE)
	p.Push(0).Op(vm.MLOAD).Push(proxyReturnSlot).Op(vm.SSTORE)
	if revertAfter {
		p.Push(0).Push(0).Op(vm.REVERT)
	} else {
		p.Op(vm.STOP)
	}
	return p.Bytes()
}

func word(v int64) common.Hash {
	return common.BigToHash(big.NewInt(v))
}

func TestCustomPrecompileContextForDirectCall(t *testing.T) {
	const txGas, requiredGas = 100_000, 700
	chainID := big.NewInt(testChainID)
	blockCtx := BlockContext{
		Number:      7,
		Time:        99,
		GasLimit:    30_000_000,
		ChainID:     chainID,
		BaseFee:     big.NewInt(3),
		BlobBaseFee: big.NewInt(2),
		Coinbase:    common.HexToAddress("0x00000000000000000000000000000000000000cb"),
		PrevRandao:  testHash(0x77),
	}
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	recorder := &contextRecorder{}
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: registryOf{probeAddr: recorder.contract(requiredGas)}}, withTestState(state))
	to := probeAddr
	rawTx := signLegacyTxWithGasPrice(t, sender.key, chainID, 0, &to, big.NewInt(5), []byte{0xaa}, txGas, big.NewInt(3))

	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockCtx, Txs: [][]byte{rawTx}})
	require.NoError(t, err)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Equal(t, uint64(oneByteTxIntrinsicGas+requiredGas), result.Txs[0].GasUsed)
	require.Equal(t, contextRecord{
		Caller:        sender.addr,
		Address:       probeAddr,
		ApparentValue: big.NewInt(5),
		GasRemaining:  txGas - oneByteTxIntrinsicGas - requiredGas,
		Block: precompiles.BlockContext{
			Number:      7,
			Time:        99,
			ChainID:     chainID,
			BaseFee:     big.NewInt(3),
			BlobBaseFee: big.NewInt(2),
			Coinbase:    blockCtx.Coinbase,
			PrevRandao:  blockCtx.PrevRandao,
		},
	}, recorder.only(t))

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(5), state.GetBalance(probeAddr))
}

func TestCustomPrecompileContextThroughCallOpcodes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		op           vm.OpCode
		callerIsEOA  bool
		value        *big.Int
		readOnly     bool
		delegateCall bool
		probeBalance int64
	}{
		{name: "CALL", op: vm.CALL, value: big.NewInt(3), probeBalance: 3},
		{name: "CALLCODE", op: vm.CALLCODE, value: big.NewInt(3), delegateCall: true},
		{name: "DELEGATECALL", op: vm.DELEGATECALL, callerIsEOA: true, delegateCall: true},
		{name: "STATICCALL", op: vm.STATICCALL, readOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewMemoryState()
			sender := newTestAccount(t, state)
			proxy := testAddress(0xa0)
			state.SetCode(proxy, precompileProxyRuntime(tc.op, probeAddr, 3, false))
			state.SetBalance(proxy, big.NewInt(100))
			recorder := &contextRecorder{}

			result := runPrecompileBlock(t, state, registryOf{probeAddr: recorder.contract(700)}, callTx(t, sender, 0, proxy, 0, []byte{0xaa}, 200_000))
			require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
			require.Equal(t, word(1), state.GetState(proxy, proxySuccessSlot))

			got := recorder.only(t)
			require.NotZero(t, got.GasRemaining)
			wantCaller := proxy
			if tc.callerIsEOA {
				wantCaller = sender.addr
			}
			require.Equal(t, wantCaller, got.Caller)
			require.Equal(t, probeAddr, got.Address)
			require.Equal(t, tc.value, got.ApparentValue)
			require.Equal(t, tc.readOnly, got.ReadOnly)
			require.Equal(t, tc.delegateCall, got.DelegateCall)
			require.Equal(t, big.NewInt(tc.probeBalance), state.GetBalance(probeAddr))
		})
	}
}

func TestCustomPrecompileCallInsideStaticCallIsReadOnly(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	outer, inner := testAddress(0xa1), testAddress(0xa2)
	state.SetCode(outer, precompileProxyRuntime(vm.STATICCALL, inner, 0, false))
	state.SetCode(inner, precompileProxyRuntime(vm.CALL, probeAddr, 0, false))
	recorder := &contextRecorder{}

	result := runPrecompileBlock(t, state, registryOf{probeAddr: recorder.contract(0)}, callTx(t, sender, 0, outer, 0, []byte{0xaa}, 2_000_000))
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)

	got := recorder.only(t)
	require.True(t, got.ReadOnly)
	require.Equal(t, inner, got.Caller)
	// The inner proxy's own SSTORE is refused under the outer STATICCALL.
	require.Equal(t, common.Hash{}, state.GetState(outer, proxySuccessSlot))
	require.Zero(t, state.GetNonce(probeAddr))
}

func TestCustomPrecompileContextMutationsDoNotLeak(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	var chainIDs []*big.Int
	mutator := scriptedContract{run: func(ctx *precompiles.Context, _ []byte) ([]byte, error) {
		chainIDs = append(chainIDs, new(big.Int).Set(ctx.Block.ChainID))
		ctx.ApparentValue.SetInt64(999)
		ctx.Block.ChainID.SetInt64(1)
		ctx.Block.BaseFee.SetInt64(1)
		return nil, nil
	}}

	result := runPrecompileBlock(t, state, registryOf{probeAddr: mutator},
		callTx(t, sender, 0, probeAddr, 5, []byte{0x01}, 100_000),
		callTx(t, sender, 1, probeAddr, 5, []byte{0x01}, 100_000),
	)
	for _, tx := range result.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
	}
	require.Equal(t, []*big.Int{big.NewInt(testChainID), big.NewInt(testChainID)}, chainIDs)
	require.Equal(t, big.NewInt(10), state.GetBalance(probeAddr))
}

func TestCustomPrecompileGasAccounting(t *testing.T) {
	const requiredGas = 5_000
	succeed := func(*precompiles.Context, []byte) ([]byte, error) { return nil, nil }
	for _, tc := range []struct {
		name        string
		txGas       uint64
		run         func(*precompiles.Context, []byte) ([]byte, error)
		wantStatus  uint64
		wantErr     error
		wantGasUsed uint64
		wantRun     bool
	}{
		{
			name:        "success charges intrinsic plus required gas",
			txGas:       100_000,
			run:         succeed,
			wantStatus:  ethtypes.ReceiptStatusSuccessful,
			wantGasUsed: oneByteTxIntrinsicGas + requiredGas,
			wantRun:     true,
		},
		{
			name:        "exactly enough gas succeeds",
			txGas:       oneByteTxIntrinsicGas + requiredGas,
			run:         succeed,
			wantStatus:  ethtypes.ReceiptStatusSuccessful,
			wantGasUsed: oneByteTxIntrinsicGas + requiredGas,
			wantRun:     true,
		},
		{
			name:        "one gas short fails before running",
			txGas:       oneByteTxIntrinsicGas + requiredGas - 1,
			run:         succeed,
			wantStatus:  ethtypes.ReceiptStatusFailed,
			wantErr:     vm.ErrOutOfGas,
			wantGasUsed: oneByteTxIntrinsicGas + requiredGas - 1,
		},
		{
			name:        "an error consumes all gas",
			txGas:       100_000,
			run:         func(*precompiles.Context, []byte) ([]byte, error) { return nil, errProbeFailed },
			wantStatus:  ethtypes.ReceiptStatusFailed,
			wantErr:     errProbeFailed,
			wantGasUsed: 100_000,
			wantRun:     true,
		},
		{
			name:        "a revert returns unused gas",
			txGas:       100_000,
			run:         func(*precompiles.Context, []byte) ([]byte, error) { return nil, vm.ErrExecutionReverted },
			wantStatus:  ethtypes.ReceiptStatusFailed,
			wantErr:     vm.ErrExecutionReverted,
			wantGasUsed: oneByteTxIntrinsicGas + requiredGas,
			wantRun:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewMemoryState()
			sender := newTestAccount(t, state)
			ran := false
			contract := scriptedContract{gas: fixedGas(requiredGas), run: func(ctx *precompiles.Context, input []byte) ([]byte, error) {
				ran = true
				return tc.run(ctx, input)
			}}

			result := runPrecompileBlock(t, state, registryOf{probeAddr: contract}, callTx(t, sender, 0, probeAddr, 0, []byte{0x01}, tc.txGas))
			require.Equal(t, tc.wantStatus, result.Txs[0].Status)
			if tc.wantErr == nil {
				require.NoError(t, result.Txs[0].Err)
			} else {
				require.ErrorIs(t, result.Txs[0].Err, tc.wantErr)
			}
			require.Equal(t, tc.wantGasUsed, result.Txs[0].GasUsed)
			require.Equal(t, tc.wantGasUsed, result.GasUsed)
			require.Equal(t, tc.wantRun, ran)
		})
	}
}

func TestCustomPrecompileRequiredGasDependsOnInput(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	contract := scriptedContract{
		gas: func(input []byte) uint64 { return 100 * uint64(len(input)) },
		run: func(*precompiles.Context, []byte) ([]byte, error) { return nil, nil },
	}
	input := []byte{0x01, 0x02, 0x03}

	result := runPrecompileBlock(t, state, registryOf{probeAddr: contract}, callTx(t, sender, 0, probeAddr, 0, input, 100_000))
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Equal(t, uint64(21_000+3*16+300), result.Txs[0].GasUsed)
}

func TestCustomPrecompileReportsRequiredGasToTheTracer(t *testing.T) {
	const suppliedGas, requiredGas = 50_000, 1_234
	type gasChange struct {
		old, new uint64
		reason   tracing.GasChangeReason
	}
	var changes []gasChange
	hooks := &tracing.Hooks{OnGasChange: func(old, new uint64, reason tracing.GasChangeReason) {
		changes = append(changes, gasChange{old: old, new: new, reason: reason})
	}}
	executor := NewExecutor(Config{CustomPrecompiles: registryOf{probeAddr: scriptedContract{
		gas: fixedGas(requiredGas),
		run: func(*precompiles.Context, []byte) ([]byte, error) { return nil, nil },
	}}})
	stateDB := newNativeStateDB(NewMemoryState())
	blockCtx := blockContext(big.NewInt(testChainID))
	evm := vm.NewEVM(buildBlockContext(blockCtx), stateDB, executor.chainConfig(blockCtx), vm.Config{Tracer: hooks}, executor.customPrecompiles)
	stateDB.SetEVM(evm)

	_, left, err := evm.Call(testAddress(0xc1), probeAddr, nil, suppliedGas, new(uint256.Int))
	require.NoError(t, err)
	require.Equal(t, uint64(suppliedGas-requiredGas), left)
	require.Contains(t, changes, gasChange{old: suppliedGas, new: suppliedGas - requiredGas, reason: tracing.GasChangeCallPrecompiledContract})
}

func TestCustomPrecompileRevertDataReachesTheCaller(t *testing.T) {
	reason := word(0x0bad)
	reverter := scriptedContract{gas: fixedGas(500), run: func(ctx *precompiles.Context, _ []byte) ([]byte, error) {
		ctx.State.SetState(ctx.Address, probeSlot, word(1))
		return reason.Bytes(), vm.ErrExecutionReverted
	}}
	registry := registryOf{probeAddr: reverter}

	t.Run("to a calling contract", func(t *testing.T) {
		state := NewMemoryState()
		sender := newTestAccount(t, state)
		proxy := testAddress(0xa3)
		state.SetCode(proxy, precompileProxyRuntime(vm.CALL, probeAddr, 0, false))

		result := runPrecompileBlock(t, state, registry, callTx(t, sender, 0, proxy, 0, []byte{0x01}, 200_000))
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
		require.Equal(t, common.Hash{}, state.GetState(proxy, proxySuccessSlot))
		require.Equal(t, word(32), state.GetState(proxy, proxyReturnSizeSlot))
		require.Equal(t, reason, state.GetState(proxy, proxyReturnSlot))
		require.Equal(t, common.Hash{}, state.GetState(probeAddr, probeSlot))
	})

	t.Run("to eth_call and gas estimation", func(t *testing.T) {
		state := NewMemoryState()
		sender := testAddress(0xc2)
		state.SetBalance(sender, big.NewInt(testFundedBalanceWei))
		executor := NewExecutor(Config{CustomPrecompiles: registry}, withTestState(state))
		to := probeAddr
		msg := callMessage(sender, &to)
		msg.Data = []byte{0x01}

		callResult, err := executor.Call(t.Context(), blockContext(big.NewInt(testChainID)), msg)
		require.NoError(t, err)
		require.ErrorIs(t, callResult.Err, vm.ErrExecutionReverted)
		require.Equal(t, reason.Bytes(), callResult.Revert())

		_, revert, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), msg, 0)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, reason.Bytes(), revert)
	})
}

// sideEffectsContract writes probeSlot, pays recipient 7 wei from its own
// balance, and logs, then returns fail.
func sideEffectsContract(recipient common.Address, fail error) scriptedContract {
	return scriptedContract{gas: fixedGas(500), run: func(ctx *precompiles.Context, _ []byte) ([]byte, error) {
		ctx.State.SetState(ctx.Address, probeSlot, word(1))
		if err := ctx.State.SubBalance(ctx.Address, big.NewInt(7)); err != nil {
			return nil, err
		}
		ctx.State.AddBalance(recipient, big.NewInt(7))
		ctx.Logs.AddLog(&ethtypes.Log{Address: ctx.Address, Topics: []common.Hash{probeSlot}})
		return nil, fail
	}}
}

func TestCustomPrecompileWritesRevertWithTheCallingFrame(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	recipient := testAddress(0xd1)
	proxy := testAddress(0xa4)
	state.SetCode(proxy, precompileProxyRuntime(vm.CALL, probeAddr, 0, true))
	state.SetBalance(probeAddr, big.NewInt(100))

	result := runPrecompileBlock(t, state, registryOf{probeAddr: sideEffectsContract(recipient, nil)}, callTx(t, sender, 0, proxy, 0, []byte{0x01}, 200_000))
	require.Equal(t, ethtypes.ReceiptStatusFailed, result.Txs[0].Status)
	require.ErrorIs(t, result.Txs[0].Err, vm.ErrExecutionReverted)
	require.Empty(t, result.Receipts[0].Logs)
	require.Equal(t, common.Hash{}, state.GetState(probeAddr, probeSlot))
	require.Equal(t, big.NewInt(100), state.GetBalance(probeAddr))
	require.Zero(t, state.GetBalance(recipient).Sign())
	require.Zero(t, state.GetNonce(probeAddr))
	require.Equal(t, common.Hash{}, state.GetState(proxy, proxySuccessSlot))
}

func TestCustomPrecompileFailureIsContainedToItsFrame(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	recipient := testAddress(0xd2)
	proxy := testAddress(0xa5)
	state.SetCode(proxy, precompileProxyRuntime(vm.CALL, probeAddr, 0, false))
	state.SetBalance(probeAddr, big.NewInt(100))

	result := runPrecompileBlock(t, state, registryOf{probeAddr: sideEffectsContract(recipient, errProbeFailed)}, callTx(t, sender, 0, proxy, 0, []byte{0x01}, 2_000_000))
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Empty(t, result.Receipts[0].Logs)
	require.Equal(t, common.Hash{}, state.GetState(proxy, proxySuccessSlot))
	require.Equal(t, word(0), state.GetState(proxy, proxyReturnSizeSlot))
	require.Equal(t, common.Hash{}, state.GetState(probeAddr, probeSlot))
	require.Equal(t, big.NewInt(100), state.GetBalance(probeAddr))
	require.Zero(t, state.GetBalance(recipient).Sign())
	require.Zero(t, state.GetNonce(probeAddr))
}

func TestCustomPrecompileBalanceOperations(t *testing.T) {
	recipient := testAddress(0xd3)
	overflow := new(big.Int).Lsh(big.NewInt(1), 256)
	for _, tc := range []struct {
		name          string
		run           func(ctx *precompiles.Context) error
		wantErr       error
		wantProbe     int64
		wantRecipient int64
		wantSlot      common.Hash
	}{
		{
			name: "transfer out of its own balance",
			run: func(ctx *precompiles.Context) error {
				if err := ctx.State.SubBalance(ctx.Address, big.NewInt(40)); err != nil {
					return err
				}
				ctx.State.AddBalance(recipient, big.NewInt(40))
				return nil
			},
			wantProbe:     60,
			wantRecipient: 40,
		},
		{
			name: "zero amounts are no-ops",
			run: func(ctx *precompiles.Context) error {
				ctx.State.AddBalance(recipient, new(big.Int))
				return ctx.State.SubBalance(ctx.Address, new(big.Int))
			},
			wantProbe: 100,
		},
		{
			name: "insufficient balance is returned to the contract",
			run: func(ctx *precompiles.Context) error {
				return ctx.State.SubBalance(ctx.Address, big.NewInt(101))
			},
			wantErr:   errInsufficientBalance,
			wantProbe: 100,
		},
		{
			name: "insufficient balance does not poison later writes",
			run: func(ctx *precompiles.Context) error {
				if err := ctx.State.SubBalance(ctx.Address, big.NewInt(101)); !errors.Is(err, errInsufficientBalance) {
					return errors.New("expected insufficient balance")
				}
				ctx.State.SetState(ctx.Address, probeSlot, word(1))
				return nil
			},
			wantProbe: 100,
			wantSlot:  word(1),
		},
		{
			name: "a negative credit fails the call even if ignored",
			run: func(ctx *precompiles.Context) error {
				ctx.State.AddBalance(recipient, big.NewInt(-1))
				ctx.State.SetState(ctx.Address, probeSlot, word(1))
				return nil
			},
			wantErr:   errPrecompileNegativeAmount,
			wantProbe: 100,
		},
		{
			name: "a negative debit fails the call",
			run: func(ctx *precompiles.Context) error {
				_ = ctx.State.SubBalance(ctx.Address, big.NewInt(-1))
				return nil
			},
			wantErr:   errPrecompileNegativeAmount,
			wantProbe: 100,
		},
		{
			name: "an amount beyond uint256 fails the call",
			run: func(ctx *precompiles.Context) error {
				ctx.State.AddBalance(recipient, overflow)
				return nil
			},
			wantErr:   errPrecompileAmountOverflow,
			wantProbe: 100,
		},
		{
			name: "a failed write takes precedence over the contract's error",
			run: func(ctx *precompiles.Context) error {
				ctx.State.AddBalance(recipient, big.NewInt(-1))
				return errProbeFailed
			},
			wantErr:   errPrecompileNegativeAmount,
			wantProbe: 100,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewMemoryState()
			sender := newTestAccount(t, state)
			state.SetBalance(probeAddr, big.NewInt(100))
			contract := scriptedContract{run: func(ctx *precompiles.Context, _ []byte) ([]byte, error) {
				return nil, tc.run(ctx)
			}}

			result := runPrecompileBlock(t, state, registryOf{probeAddr: contract}, callTx(t, sender, 0, probeAddr, 0, []byte{0x01}, 100_000))
			if tc.wantErr == nil {
				require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
			} else {
				require.Equal(t, ethtypes.ReceiptStatusFailed, result.Txs[0].Status)
				require.ErrorIs(t, result.Txs[0].Err, tc.wantErr)
			}
			require.Equal(t, big.NewInt(tc.wantProbe), state.GetBalance(probeAddr))
			require.Equal(t, big.NewInt(tc.wantRecipient), state.GetBalance(recipient))
			require.Equal(t, tc.wantSlot, state.GetState(probeAddr, probeSlot))
		})
	}
}

func TestCustomPrecompileReadsAndWritesOtherAccounts(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	other := testAddress(0xd4)
	contract := testAddress(0xd5)
	state.SetBalance(other, big.NewInt(42))
	state.SetNonce(other, 3)
	state.SetCode(contract, []byte{0x00})
	state.SetState(contract, probeSlot, word(8))
	accounts := scriptedContract{run: func(ctx *precompiles.Context, input []byte) ([]byte, error) {
		if len(input) == 0 {
			var out []byte
			out = append(out, common.BigToHash(ctx.State.GetBalance(other)).Bytes()...)
			out = append(out, word(int64(ctx.State.GetNonce(other))).Bytes()...) //nolint:gosec // test nonce is small.
			out = append(out, ctx.State.GetState(contract, probeSlot).Bytes()...)
			return append(out, ctx.State.GetCode(contract)...), nil
		}
		ctx.State.SetNonce(other, 9)
		ctx.State.SetState(contract, probeSlot, word(10))
		return nil, nil
	}}
	registry := registryOf{probeAddr: accounts}
	to := probeAddr

	callResult, err := NewExecutor(Config{CustomPrecompiles: registry}, withTestState(state)).Call(t.Context(), blockContext(big.NewInt(testChainID)), callMessage(sender.addr, &to))
	require.NoError(t, err)
	require.NoError(t, callResult.Err)
	want := append(append(append(word(42).Bytes(), word(3).Bytes()...), word(8).Bytes()...), 0x00)
	require.Equal(t, want, callResult.ReturnData)

	result := runPrecompileBlock(t, state, registry, callTx(t, sender, 0, probeAddr, 0, []byte{0x01}, 100_000))
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Equal(t, uint64(9), state.GetNonce(other))
	require.Equal(t, word(10), state.GetState(contract, probeSlot))
}

func TestCustomPrecompileStaticCallRejectsEveryWrite(t *testing.T) {
	caller := testAddress(0xc3)
	recipient := testAddress(0xd6)
	for _, tc := range []struct {
		name  string
		write func(ctx *precompiles.Context) error
	}{
		{name: "SetState", write: func(ctx *precompiles.Context) error {
			ctx.State.SetState(ctx.Address, probeSlot, word(1))
			return nil
		}},
		{name: "SetNonce", write: func(ctx *precompiles.Context) error {
			ctx.State.SetNonce(recipient, 9)
			return nil
		}},
		{name: "AddBalance", write: func(ctx *precompiles.Context) error {
			ctx.State.AddBalance(recipient, big.NewInt(1))
			return nil
		}},
		{name: "SubBalance", write: func(ctx *precompiles.Context) error {
			if err := ctx.State.SubBalance(ctx.Address, big.NewInt(1)); !errors.Is(err, vm.ErrWriteProtection) {
				return errors.New("expected write protection")
			}
			return nil
		}},
		{name: "AddLog", write: func(ctx *precompiles.Context) error {
			ctx.Logs.AddLog(&ethtypes.Log{Address: ctx.Address})
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewMemoryState()
			state.SetBalance(probeAddr, big.NewInt(100))
			reads := 0
			contract := scriptedContract{run: func(ctx *precompiles.Context, _ []byte) ([]byte, error) {
				require.True(t, ctx.ReadOnly)
				ctx.State.GetBalance(ctx.Address)
				ctx.State.GetState(ctx.Address, probeSlot)
				reads++
				return nil, tc.write(ctx)
			}}
			executor := NewExecutor(Config{CustomPrecompiles: registryOf{probeAddr: contract}})
			stateDB := newNativeStateDB(state)
			blockCtx := blockContext(big.NewInt(testChainID))
			evm := vm.NewEVM(buildBlockContext(blockCtx), stateDB, executor.chainConfig(blockCtx), vm.Config{}, executor.customPrecompiles)
			stateDB.SetEVM(evm)

			_, left, err := evm.StaticCall(caller, probeAddr, nil, 10_000)
			require.ErrorIs(t, err, vm.ErrWriteProtection)
			require.Zero(t, left)
			require.Equal(t, 1, reads)
			require.Equal(t, big.NewInt(100), stateDB.GetBalance(probeAddr).ToBig())
			require.Zero(t, stateDB.GetBalance(recipient).Sign())
			require.Zero(t, stateDB.GetNonce(recipient))
			require.Zero(t, stateDB.GetNonce(probeAddr))
			require.Equal(t, common.Hash{}, stateDB.GetState(probeAddr, probeSlot))
			require.Empty(t, stateDB.Logs())
		})
	}
}

// slotIncrementer adds one to probeSlot on every call, and fails when the input is 0xff.
var slotIncrementer = scriptedContract{gas: fixedGas(500), run: func(ctx *precompiles.Context, input []byte) ([]byte, error) {
	next := new(big.Int).Add(ctx.State.GetState(ctx.Address, probeSlot).Big(), big.NewInt(1))
	ctx.State.SetState(ctx.Address, probeSlot, common.BigToHash(next))
	if len(input) > 0 && input[0] == 0xff {
		return nil, errProbeFailed
	}
	return nil, nil
}}

func TestCustomPrecompileAccountMaterialization(t *testing.T) {
	for _, tc := range []struct {
		name      string
		seed      func(*MemoryState)
		input     byte
		wantNonce uint64
		wantSlot  common.Hash
	}{
		{name: "an absent account gets nonce 1", seed: func(*MemoryState) {}, input: 0x01, wantNonce: 1, wantSlot: word(2)},
		{name: "a balance-only account gets nonce 1", seed: func(s *MemoryState) { s.SetBalance(probeAddr, big.NewInt(5)) }, input: 0x01, wantNonce: 1, wantSlot: word(2)},
		{name: "an existing nonce is kept", seed: func(s *MemoryState) { s.SetNonce(probeAddr, 5) }, input: 0x01, wantNonce: 5, wantSlot: word(2)},
		{name: "an account with code keeps nonce 0", seed: func(s *MemoryState) { s.SetCode(probeAddr, []byte{0x00}) }, input: 0x01, wantNonce: 0, wantSlot: word(2)},
		{name: "a failed call leaves the account absent", seed: func(*MemoryState) {}, input: 0xff, wantNonce: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewMemoryState()
			sender := newTestAccount(t, state)
			tc.seed(state)
			registry := registryOf{probeAddr: slotIncrementer}

			runPrecompileBlock(t, state, registry, callTx(t, sender, 0, probeAddr, 0, []byte{tc.input}, 100_000))
			runPrecompileBlock(t, state, registry, callTx(t, sender, 1, probeAddr, 0, []byte{tc.input}, 100_000))
			require.Equal(t, tc.wantNonce, state.GetNonce(probeAddr))
			require.Equal(t, tc.wantSlot, state.GetState(probeAddr, probeSlot))
		})
	}
}

func TestCustomPrecompileStaticCallDoesNotMaterialize(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	proxy := testAddress(0xa6)
	state.SetCode(proxy, precompileProxyRuntime(vm.STATICCALL, probeAddr, 0, false))
	reader := scriptedContract{run: func(ctx *precompiles.Context, _ []byte) ([]byte, error) {
		return ctx.State.GetState(ctx.Address, probeSlot).Bytes(), nil
	}}

	result := runPrecompileBlock(t, state, registryOf{probeAddr: reader}, callTx(t, sender, 0, proxy, 0, []byte{0x01}, 200_000))
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Equal(t, word(1), state.GetState(proxy, proxySuccessSlot))
	require.Zero(t, state.GetNonce(probeAddr))
}

func TestCustomPrecompileUnresolvedAddressFailsClosed(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	proxy := testAddress(0xa7)
	state.SetCode(proxy, precompileProxyRuntime(vm.CALL, unresolvedAddr, 0, false))
	registry := registryOf{probeAddr: slotIncrementer, unresolvedAddr: nil}

	result := runPrecompileBlock(t, state, registry,
		callTx(t, sender, 0, unresolvedAddr, 0, []byte{0x01}, 100_000),
		callTx(t, sender, 1, proxy, 0, []byte{0x01}, 2_000_000),
		callTx(t, sender, 2, probeAddr, 0, []byte{0x01}, 100_000),
	)
	require.Equal(t, ethtypes.ReceiptStatusFailed, result.Txs[0].Status)
	require.ErrorIs(t, result.Txs[0].Err, precompiles.ErrCustomPrecompilesOpen)
	require.Equal(t, uint64(100_000), result.Txs[0].GasUsed)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[1].Status)
	require.Equal(t, common.Hash{}, state.GetState(proxy, proxySuccessSlot))
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[2].Status)
	require.Equal(t, word(1), state.GetState(probeAddr, probeSlot))
	require.Zero(t, state.GetNonce(unresolvedAddr))
}

func TestCustomPrecompileMapIgnoresEmptyRegistries(t *testing.T) {
	require.Nil(t, customPrecompileMap(nil))
	require.Nil(t, customPrecompileMap(registryOf{}))
	require.Len(t, customPrecompileMap(registryOf{probeAddr: slotIncrementer, unresolvedAddr: nil}), 2)
}

func TestCustomPrecompileCannotShadowBuiltinPrecompiles(t *testing.T) {
	identity := common.BytesToAddress([]byte{0x04})
	state := NewMemoryState()
	sender := testAddress(0xc4)
	state.SetBalance(sender, big.NewInt(testFundedBalanceWei))
	recorder := &contextRecorder{}
	executor := NewExecutor(Config{CustomPrecompiles: registryOf{identity: recorder.contract(0)}}, withTestState(state))
	msg := callMessage(sender, &identity)
	msg.Data = []byte{0xde, 0xad}

	result, err := executor.Call(t.Context(), blockContext(big.NewInt(testChainID)), msg)
	require.NoError(t, err)
	require.NoError(t, result.Err)
	require.Equal(t, msg.Data, result.ReturnData)
	require.Zero(t, recorder.count())
}

func TestCustomPrecompileCallAndEstimateDoNotPersist(t *testing.T) {
	state := NewMemoryState()
	sender := newTestAccount(t, state)
	state.SetState(probeAddr, probeSlot, word(4))
	state.SetNonce(probeAddr, 1)
	registry := registryOf{probeAddr: slotIncrementer}
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: registry}, withTestState(state))
	blockCtx := blockContext(big.NewInt(testChainID))
	to := probeAddr
	msg := callMessage(sender.addr, &to)
	msg.Data = []byte{0x01}

	for range 2 {
		result, err := executor.Call(t.Context(), blockCtx, msg)
		require.NoError(t, err)
		require.NoError(t, result.Err)
		require.Equal(t, uint64(oneByteTxIntrinsicGas+500), result.UsedGas)
	}
	estimate, _, err := executor.EstimateGas(t.Context(), blockCtx, msg, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, estimate, uint64(oneByteTxIntrinsicGas+500))
	require.Equal(t, word(4), state.GetState(probeAddr, probeSlot))

	failing := *msg
	failing.Data = []byte{0xff}
	_, _, err = executor.EstimateGas(t.Context(), blockCtx, &failing, 0)
	require.Error(t, err)

	block := runPrecompileBlock(t, state, registry, callTx(t, sender, 0, probeAddr, 0, []byte{0x01}, estimate))
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, block.Txs[0].Status)
	require.Equal(t, word(5), state.GetState(probeAddr, probeSlot))
}

func TestCustomPrecompileAdapterDelegatesToTheContract(t *testing.T) {
	recorder := &contextRecorder{}
	adapter := customPrecompile{address: probeAddr, contract: scriptedContract{
		gas: func(input []byte) uint64 { return 7 * uint64(len(input)) },
		run: recorder.contract(0).run,
	}}
	require.Equal(t, uint64(21), adapter.RequiredGas([]byte{1, 2, 3}))

	executor := NewExecutor(Config{})
	stateDB := newNativeStateDB(NewMemoryState())
	blockCtx := blockContext(big.NewInt(testChainID))
	evm := vm.NewEVM(buildBlockContext(blockCtx), stateDB, executor.chainConfig(blockCtx), vm.Config{}, nil)
	stateDB.SetEVM(evm)
	caller := testAddress(0xc5)

	_, err := adapter.Run(evm, caller, caller, []byte{0x01}, big.NewInt(2), true, false, nil)
	require.NoError(t, err)
	got := recorder.only(t)
	require.Equal(t, caller, got.Caller)
	require.Equal(t, probeAddr, got.Address)
	require.Equal(t, big.NewInt(2), got.ApparentValue)
	require.True(t, got.ReadOnly)
	require.Zero(t, got.GasRemaining)
}

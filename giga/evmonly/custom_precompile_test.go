package evmonly

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

const counterPrecompileGas = 1_000

var (
	counterPrecompileAddr = common.HexToAddress("0x0000000000000000000000000000000000001002")
	counterSharedSlot     = common.Hash{0xff}
	errCounterRefused     = errors.New("counter refused")
)

// Inputs understood by counterPrecompile.
const (
	counterOwn    byte = 0x01 // increment the caller's slot
	counterShared byte = 0x02 // increment the caller's slot and the shared slot
	counterRefuse byte = 0x03 // increment the caller's slot, then fail
)

// counterPrecompile keeps one counter per caller, and one shared counter, in its
// own storage, and logs every increment.
type counterPrecompile struct{}

func (counterPrecompile) RequiredGas([]byte) uint64 { return counterPrecompileGas }

func (counterPrecompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	own := common.BytesToHash(ctx.Caller.Bytes())
	if len(input) == 0 {
		value := ctx.State.GetState(ctx.Address, own)
		return value.Bytes(), nil
	}
	increment(ctx, own)
	switch input[0] {
	case counterShared:
		increment(ctx, counterSharedSlot)
	case counterRefuse:
		return nil, errCounterRefused
	}
	ctx.Logs.AddLog(&ethtypes.Log{Address: ctx.Address, Topics: []common.Hash{own}})
	return nil, nil
}

func increment(ctx *precompiles.Context, slot common.Hash) {
	next := new(big.Int).Add(ctx.State.GetState(ctx.Address, slot).Big(), big.NewInt(1))
	ctx.State.SetState(ctx.Address, slot, common.BigToHash(next))
}

type counterRegistry struct{}

func (counterRegistry) Get(addr common.Address) (precompiles.Contract, bool) {
	if addr != counterPrecompileAddr {
		return nil, false
	}
	return counterPrecompile{}, true
}

func (counterRegistry) Addresses() []common.Address {
	return []common.Address{counterPrecompileAddr}
}

type counterCaller struct {
	key  *ecdsa.PrivateKey
	addr common.Address
}

func newCounterCallers(t *testing.T, n int, states ...*MemoryState) []counterCaller {
	t.Helper()
	callers := make([]counterCaller, n)
	for i := range callers {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		callers[i] = counterCaller{key: key, addr: crypto.PubkeyToAddress(key.PublicKey)}
		for _, state := range states {
			state.SetBalance(callers[i].addr, big.NewInt(testFundedBalanceWei))
		}
	}
	return callers
}

func counterTx(t *testing.T, caller counterCaller, input byte) []byte {
	t.Helper()
	to := counterPrecompileAddr
	return signLegacyTxWithGasPrice(t, caller.key, big.NewInt(testChainID), 0, &to, big.NewInt(0), []byte{input}, 100_000, big.NewInt(0))
}

func counterValue(state *MemoryState, slot common.Hash) *big.Int {
	return state.GetState(counterPrecompileAddr, slot).Big()
}

func TestCustomPrecompileOCCMatchesSequential(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     byte
		conflicts bool
	}{
		{name: "disjoint slots", input: counterOwn},
		{name: "shared slot", input: counterShared, conflicts: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const txCount = 16
			seqState := NewMemoryState()
			occState := NewMemoryState()
			callers := newCounterCallers(t, txCount, seqState, occState)
			seqState.SetNonce(counterPrecompileAddr, 1)
			occState.SetNonce(counterPrecompileAddr, 1)
			rawTxs := make([][]byte, txCount)
			for i, caller := range callers {
				rawTxs[i] = counterTx(t, caller, tc.input)
			}
			req := BlockRequest{Context: blockContext(big.NewInt(testChainID)), Txs: rawTxs}

			seqCfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: counterRegistry{}}
			occCfg := seqCfg
			occCfg.OCCWorkers = 4
			seqResult, err := NewExecutor(seqCfg, withTestState(seqState)).ExecuteBlock(t.Context(), req)
			require.NoError(t, err)
			occResult, err := NewExecutor(occCfg, withTestState(occState)).ExecuteBlock(t.Context(), req)
			require.NoError(t, err)

			requireOCCRan(t, occCfg, occResult)
			if tc.conflicts {
				require.NotZero(t, occResult.OCCStats.ConflictCount)
			} else {
				require.Zero(t, occResult.OCCStats.ConflictCount, "%+v", occResult.OCCStats.ConflictSamples)
			}
			require.Equal(t, seqResult.GasUsed, occResult.GasUsed)
			for i := range txCount {
				require.Equal(t, ethtypes.ReceiptStatusSuccessful, occResult.Txs[i].Status)
				require.Equal(t, seqResult.Receipts[i].Logs, occResult.Receipts[i].Logs)
				require.Len(t, occResult.Receipts[i].Logs, 1)
			}

			seqState.ApplyChangeSet(seqResult.ChangeSet)
			occState.ApplyChangeSet(occResult.ChangeSet)
			for _, caller := range callers {
				own := common.BytesToHash(caller.addr.Bytes())
				require.Equal(t, big.NewInt(1), counterValue(occState, own))
				require.Equal(t, counterValue(seqState, own), counterValue(occState, own))
			}
			require.Equal(t, counterValue(seqState, counterSharedSlot), counterValue(occState, counterSharedSlot))
			if tc.conflicts {
				require.Equal(t, big.NewInt(txCount), counterValue(occState, counterSharedSlot))
			}
		})
	}
}

func TestCustomPrecompileStorageOutlivesItsFirstCall(t *testing.T) {
	for _, occ := range []bool{false, true} {
		t.Run(map[bool]string{false: "sequential", true: "occ"}[occ], func(t *testing.T) {
			const txCount = 8
			state := NewMemoryState()
			callers := newCounterCallers(t, txCount, state)
			rawTxs := make([][]byte, txCount)
			for i, caller := range callers {
				rawTxs[i] = counterTx(t, caller, counterShared)
			}
			cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: counterRegistry{}}
			if occ {
				cfg.OCCWorkers = 4
			}
			result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
				Context: blockContext(big.NewInt(testChainID)),
				Txs:     rawTxs,
			})
			require.NoError(t, err)
			requireOCCRan(t, cfg, result)

			state.ApplyChangeSet(result.ChangeSet)
			require.Equal(t, uint64(1), state.GetNonce(counterPrecompileAddr))
			require.Equal(t, big.NewInt(txCount), counterValue(state, counterSharedSlot))
			for _, caller := range callers {
				require.Equal(t, big.NewInt(1), counterValue(state, common.BytesToHash(caller.addr.Bytes())))
			}
		})
	}
}

// TestCustomPrecompileKeepsStorageHeldBeforeItsFirstWrite seeds a precompile
// account with storage but no nonce, code or balance, as genesis could, and
// requires its first state-changing calls to read and keep that storage.
func TestCustomPrecompileKeepsStorageHeldBeforeItsFirstWrite(t *testing.T) {
	for _, occ := range []bool{false, true} {
		t.Run(map[bool]string{false: "sequential", true: "occ"}[occ], func(t *testing.T) {
			const txCount = 8
			const seeded = 7
			untouched := common.HexToHash("0xdead")
			state := NewMemoryState()
			callers := newCounterCallers(t, txCount, state)
			state.SetState(counterPrecompileAddr, counterSharedSlot, common.BigToHash(big.NewInt(seeded)))
			state.SetState(counterPrecompileAddr, untouched, common.BigToHash(big.NewInt(seeded)))
			rawTxs := make([][]byte, txCount)
			for i, caller := range callers {
				rawTxs[i] = counterTx(t, caller, counterShared)
			}
			cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: counterRegistry{}}
			if occ {
				cfg.OCCWorkers = 4
			}
			result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
				Context: blockContext(big.NewInt(testChainID)),
				Txs:     rawTxs,
			})
			require.NoError(t, err)
			requireOCCRan(t, cfg, result)
			require.Empty(t, result.ChangeSet.StorageClears)

			state.ApplyChangeSet(result.ChangeSet)
			require.Equal(t, uint64(1), state.GetNonce(counterPrecompileAddr))
			require.Equal(t, big.NewInt(seeded+txCount), counterValue(state, counterSharedSlot))
			require.Equal(t, big.NewInt(seeded), counterValue(state, untouched))
		})
	}
}

func TestCustomPrecompileFailureRevertsItsWrites(t *testing.T) {
	state := NewMemoryState()
	caller := newCounterCallers(t, 1, state)[0]
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: counterRegistry{}}, withTestState(state))

	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(big.NewInt(testChainID)),
		Txs:     [][]byte{counterTx(t, caller, counterRefuse)},
	})
	require.NoError(t, err)
	require.Equal(t, ethtypes.ReceiptStatusFailed, result.Txs[0].Status)
	require.ErrorIs(t, result.Txs[0].Err, errCounterRefused)

	state.ApplyChangeSet(result.ChangeSet)
	require.Zero(t, counterValue(state, common.BytesToHash(caller.addr.Bytes())).Sign())
}

func TestCustomPrecompileStaticCall(t *testing.T) {
	caller := testAddress(0xc1)
	own := common.BytesToHash(caller.Bytes())
	state := NewMemoryState()
	state.SetState(counterPrecompileAddr, own, common.BigToHash(big.NewInt(7)))
	executor := NewExecutor(Config{CustomPrecompiles: counterRegistry{}})
	stateDB := newNativeStateDB(state)
	chainConfig := executor.chainConfig(blockContext(big.NewInt(testChainID)))
	evm := vm.NewEVM(buildBlockContext(blockContext(big.NewInt(testChainID))), stateDB, chainConfig, vm.Config{}, executor.customPrecompiles)
	stateDB.SetEVM(evm)

	t.Run("reads", func(t *testing.T) {
		out, left, err := evm.StaticCall(caller, counterPrecompileAddr, nil, 10_000)
		require.NoError(t, err)
		require.Equal(t, uint64(10_000-counterPrecompileGas), left)
		require.Equal(t, big.NewInt(7), new(big.Int).SetBytes(out))
	})
	t.Run("refuses writes", func(t *testing.T) {
		_, _, err := evm.StaticCall(caller, counterPrecompileAddr, []byte{counterOwn}, 10_000)
		require.ErrorIs(t, err, vm.ErrWriteProtection)
		require.Equal(t, common.BigToHash(big.NewInt(7)), stateDB.GetState(counterPrecompileAddr, own))
	})
	t.Run("charges required gas", func(t *testing.T) {
		_, _, err := evm.StaticCall(caller, counterPrecompileAddr, nil, counterPrecompileGas-1)
		require.ErrorIs(t, err, vm.ErrOutOfGas)
	})
}

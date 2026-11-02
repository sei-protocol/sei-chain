package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// staticCallCode returns runtime bytecode that STATICCALLs target with no input and stops.
func staticCallCode(target common.Address) []byte {
	code := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73} // retSize, retOffset, argsSize, argsOffset, PUSH20
	code = append(code, target.Bytes()...)
	return append(code, 0x5a, 0xfa, 0x00) // GAS, STATICCALL, STOP
}

// selfBalanceCode reads the executing contract's own balance: ADDRESS, BALANCE, STOP.
var selfBalanceCode = []byte{0x30, 0x31, 0x00}

// sstoreCode returns runtime bytecode that stores value at slot key.
func sstoreCode(key common.Hash, value byte) []byte {
	code := []byte{0x60, value, 0x7f} // PUSH1 value, PUSH32 key
	code = append(code, key.Bytes()...)
	return append(code, 0x55, 0x00) // SSTORE, STOP
}

func TestExecutorCreateAccessList(t *testing.T) {
	chainID := big.NewInt(testChainID)
	sender := testAddress(0xa1)
	slot := testHash(0x11)
	reader := testAddress(0xc1)
	caller := testAddress(0xc2)
	reverter := testAddress(0xc3)
	selfBalance := testAddress(0xc4)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	state.SetCode(selfBalance, selfBalanceCode)
	state.SetCode(reader, sloadReturnCode(slot))
	state.SetCode(caller, staticCallCode(reader))
	state.SetCode(reverter, revertReasonRuntime("no"))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	ecrecover := common.BytesToAddress([]byte{0x01})
	for _, tc := range []struct {
		name    string
		to      common.Address
		value   *big.Int
		want    ethtypes.AccessList
		wantErr error
		wantGas uint64
	}{
		{name: "callee's slot through a contract", to: caller, want: ethtypes.AccessList{{Address: reader, StorageKeys: []common.Hash{slot}}}},
		{name: "recipient's slots are listed", to: reader, want: ethtypes.AccessList{{Address: reader, StorageKeys: []common.Hash{slot}}}},
		{name: "recipient's address is left out", to: selfBalance, want: ethtypes.AccessList{}},
		{name: "plain transfer", to: testAddress(0xd1), value: big.NewInt(1), want: ethtypes.AccessList{}, wantGas: params.TxGas},
		{name: "precompile call", to: ecrecover, want: ethtypes.AccessList{}},
		{name: "reverting call reports its error", to: reverter, want: ethtypes.AccessList{}, wantErr: vm.ErrExecutionReverted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := callMessage(sender, &tc.to)
			if tc.value != nil {
				msg.Value = tc.value
			}

			accessList, result, err := executor.CreateAccessList(t.Context(), blockContext(chainID), msg)

			require.NoError(t, err)
			require.Equal(t, tc.want, accessList)
			require.ErrorIs(t, result.Err, tc.wantErr)
			if tc.wantGas != 0 {
				require.Equal(t, tc.wantGas, result.UsedGas)
			}
		})
	}
}

func TestExecutorCreateAccessListLeavesOutTheCreatedContract(t *testing.T) {
	chainID := big.NewInt(testChainID)
	sender := testAddress(0xa1)
	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	state.SetNonce(sender, 3)
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	msg := callMessage(sender, nil)
	msg.Data = selfBalanceCode // the constructor reads its own balance

	accessList, result, err := executor.CreateAccessList(t.Context(), blockContext(chainID), msg)

	require.NoError(t, err)
	require.NoError(t, result.Err)
	require.Equal(t, ethtypes.AccessList{}, accessList)
	require.Empty(t, state.GetCode(crypto.CreateAddress(sender, 3)))
}

func TestExecutorCreateAccessListRefusals(t *testing.T) {
	chainID := big.NewInt(testChainID)
	sender := testAddress(0xa1)
	target := testAddress(0xb2)
	store := NewMemoryStore(NewMemoryState())

	for _, tc := range []struct {
		name     string
		executor *Executor
		msg      func() *core.Message
		wantErr  string
	}{
		{name: "missing state store", executor: NewExecutor(Config{}), msg: func() *core.Message { return callMessage(sender, &target) }, wantErr: errMissingStateStore.Error()},
		{name: "unappliable message", executor: NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet)),
			msg: func() *core.Message {
				msg := callMessage(sender, &target)
				msg.Value = big.NewInt(1) // the sender holds nothing
				return msg
			}, wantErr: "failed to apply transaction"},
		{name: "more authorizations than gas", executor: NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet)),
			msg: func() *core.Message {
				msg := callMessage(sender, &target)
				msg.GasLimit = params.CallNewAccountGas
				msg.SetCodeAuthorizations = make([]ethtypes.SetCodeAuthorization, 2)
				return msg
			}, wantErr: "insufficient gas to process all authorizations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.executor.CreateAccessList(t.Context(), blockContext(chainID), tc.msg())
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestExecutorEstimateGasAfterCalls(t *testing.T) {
	chainID := big.NewInt(testChainID)
	sender := testAddress(0xa1)
	slot := testHash(0x11)
	writer := testAddress(0xc1)
	reverter := testAddress(0xc3)

	newExecutor := func() (*Executor, *MemoryState) {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
		state.SetCode(writer, sstoreCode(slot, 1))
		state.SetCode(reverter, revertReasonRuntime("no"))
		store := NewMemoryStore(state)
		return NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet)), state
	}
	executor, _ := newExecutor()
	fresh, _, err := executor.EstimateGas(t.Context(), blockContext(chainID), callMessage(sender, &writer), 0)
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		calls   []*core.Message
		cheaper bool
		wantErr string
	}{
		{name: "no calls matches eth_estimateGas"},
		{name: "a call that writes the slot first makes the write cheaper", calls: []*core.Message{callMessage(sender, &writer)}, cheaper: true},
		{name: "a reverting call does not stop the estimate", calls: []*core.Message{callMessage(sender, &reverter)}},
		{name: "an unappliable call fails the estimate", calls: []*core.Message{func() *core.Message {
			msg := callMessage(testAddress(0xee), &writer)
			msg.Value = big.NewInt(1)
			return msg
		}()}, wantErr: "call 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor, state := newExecutor()

			estimate, revert, err := executor.EstimateGasAfterCalls(t.Context(), blockContext(chainID), tc.calls, callMessage(sender, &writer), 0)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Empty(t, revert)
			if tc.cheaper {
				require.Less(t, estimate, fresh)
			} else {
				require.Equal(t, fresh, estimate)
			}
			require.Equal(t, common.Hash{}, state.GetState(writer, slot), "the calls persisted state")
		})
	}
}

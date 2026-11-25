package executor_test

// Pins giga executor propagation of fail-fast (vm.AbortError) custom precompile calls.

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	gigaexecutor "github.com/sei-protocol/sei-chain/giga/executor"
	gigaprecompiles "github.com/sei-protocol/sei-chain/giga/executor/precompiles"
	gigautils "github.com/sei-protocol/sei-chain/giga/executor/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	behaviorChainID     = 713714
	behaviorGasLimit    = 1_000_000
	behaviorBlockNumber = 100
	behaviorBlockTime   = 1_700_000_000
)

var (
	// Anvil account 0; only the address is used.
	behaviorSender  = crypto.PubkeyToAddress(mustAnvilKey().PublicKey)
	behaviorBank    = common.HexToAddress("0x0000000000000000000000000000000000001001")
	behaviorCaller  = common.HexToAddress("0x00000000000000000000000000000000000c0001")
	behaviorOuter   = common.HexToAddress("0x00000000000000000000000000000000000c0002")
	behaviorFactory = common.HexToAddress("0x00000000000000000000000000000000000c0003")
	slotMarker      = common.BigToHash(big.NewInt(0))
	slotSuccess     = common.BigToHash(big.NewInt(1))
)

func mustAnvilKey() *ecdsa.PrivateKey {
	key, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		panic(err)
	}
	return key
}

// bankSupplyUsei is the ABI encoding of IBank.supply("usei").
func bankSupplyUsei() []byte {
	data := append([]byte{}, crypto.Keccak256([]byte("supply(string)"))[:4]...)
	data = append(data, common.LeftPadBytes([]byte{0x20}, 32)...)
	data = append(data, common.LeftPadBytes([]byte{0x04}, 32)...)
	return append(data, common.RightPadBytes([]byte("usei"), 32)...)
}

// precompileCallProgram sets slot0=1, calls target via op, then stores success (slot1) and RETURNDATASIZE (slot2).
func precompileCallProgram(op vm.OpCode, target common.Address, input []byte) []byte {
	inLen := uint16(len(input)) //nolint:gosec // test inputs are tiny.
	code := []byte{byte(vm.PUSH1), 0x01, byte(vm.PUSH1), 0x00, byte(vm.SSTORE)}
	code = append(code, byte(vm.PUSH2), byte(inLen>>8), byte(inLen))
	offsetPos := len(code) + 1
	code = append(code, byte(vm.PUSH2), 0x00, 0x00, byte(vm.PUSH1), 0x00, byte(vm.CODECOPY))
	code = append(code, byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00) // retSize, retOffset
	code = append(code, byte(vm.PUSH2), byte(inLen>>8), byte(inLen), byte(vm.PUSH1), 0x00)
	if op == vm.CALL || op == vm.CALLCODE {
		code = append(code, byte(vm.PUSH1), 0x00)
	}
	code = append(code, byte(vm.PUSH20))
	code = append(code, target.Bytes()...)
	code = append(code, byte(vm.GAS), byte(op))
	code = append(code, byte(vm.PUSH1), 0x01, byte(vm.SSTORE))
	code = append(code, byte(vm.RETURNDATASIZE), byte(vm.PUSH1), 0x02, byte(vm.SSTORE), byte(vm.STOP))
	dataOffset := uint16(len(code)) //nolint:gosec // bounded.
	code[offsetPos] = byte(dataOffset >> 8)
	code[offsetPos+1] = byte(dataOffset)
	return append(code, input...)
}

// createProgram deploys childInit via CREATE or CREATE2 (salt 0) and stores the address in slot3.
func createProgram(op vm.OpCode, childInit []byte) []byte {
	l := uint16(len(childInit)) //nolint:gosec // bounded.
	code := []byte{byte(vm.PUSH2), byte(l >> 8), byte(l)}
	offsetPos := len(code) + 1
	code = append(code, byte(vm.PUSH2), 0x00, 0x00, byte(vm.PUSH1), 0x00, byte(vm.CODECOPY))
	if op == vm.CREATE2 {
		code = append(code, byte(vm.PUSH1), 0x00)
	}
	code = append(code, byte(vm.PUSH2), byte(l>>8), byte(l), byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(op))
	code = append(code, byte(vm.PUSH1), 0x03, byte(vm.SSTORE), byte(vm.STOP))
	dataOffset := uint16(len(code)) //nolint:gosec // bounded.
	code[offsetPos] = byte(dataOffset >> 8)
	code[offsetPos+1] = byte(dataOffset)
	return append(code, childInit...)
}

// newBehaviorStateDB builds a committed StateDB with a funded sender and the given code.
func newBehaviorStateDB(t *testing.T, code map[common.Address][]byte) *gethstate.StateDB {
	t.Helper()
	db := gethstate.NewDatabase(triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil), nil)
	sdb, err := gethstate.New(ethtypes.EmptyRootHash, db)
	require.NoError(t, err)
	sdb.SetBalance(behaviorSender, uint256.NewInt(1_000_000_000_000_000_000), tracing.BalanceChangeUnspecified)
	for addr, c := range code {
		sdb.SetCode(addr, c, tracing.CodeChangeUnspecified)
		sdb.SetNonce(addr, 1, tracing.NonceChangeUnspecified)
	}
	root, err := sdb.Commit(params.Rules{IsEIP158: true}, 0)
	require.NoError(t, err)
	sdb, err = gethstate.New(root, db)
	require.NoError(t, err)
	return sdb
}

// newBehaviorExecutor builds a giga executor with Sei chain config and fail-fast custom precompiles.
func newBehaviorExecutor(sdb vm.StateDB) *gigaexecutor.Executor {
	chainCfg := evmtypes.DefaultChainConfig().EthereumConfig(big.NewInt(behaviorChainID))
	random := common.Hash{0x01}
	blockCtx := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.HexToAddress("0x00000000000000000000000000000000000000cb"),
		GasLimit:    10_000_000,
		BlockNumber: big.NewInt(behaviorBlockNumber),
		Time:        behaviorBlockTime,
		Difficulty:  big.NewInt(0),
		BaseFee:     big.NewInt(0),
		BlobBaseFee: big.NewInt(1),
		Random:      &random,
	}
	return gigaexecutor.NewGethExecutor(blockCtx, sdb, chainCfg, vm.Config{}, gigaprecompiles.AllCustomPrecompilesFailFast)
}

func behaviorTx(to *common.Address, data []byte) *ethtypes.Transaction {
	return ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   big.NewInt(behaviorChainID),
		Nonce:     0,
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(0),
		Gas:       behaviorGasLimit,
		To:        to,
		Value:     big.NewInt(0),
		Data:      data,
	})
}

func TestGigaExecutorFailFastAbortPropagation_Behavior(t *testing.T) {
	input := bankSupplyUsei()
	createdByTx := crypto.CreateAddress(behaviorSender, 0)

	// usedGas: direct call is intrinsic only; nested aborts keep forwarded gas (all but 1/64).
	tests := []struct {
		name    string
		code    map[common.Address][]byte
		to      *common.Address
		data    []byte
		usedGas uint64
		// partial state left in the StateDB on abort
		markerAddr common.Address
		wantMarker common.Hash
	}{
		{
			name:       "direct tx to precompile",
			to:         &behaviorBank,
			data:       input,
			usedGas:    22_300,
			markerAddr: behaviorCaller,
			wantMarker: common.Hash{},
		},
		{
			name:       "CALL",
			code:       map[common.Address][]byte{behaviorCaller: precompileCallProgram(vm.CALL, behaviorBank, input)},
			to:         &behaviorCaller,
			usedGas:    985_051,
			markerAddr: behaviorCaller,
			wantMarker: common.BigToHash(big.NewInt(1)),
		},
		{
			name:       "STATICCALL",
			code:       map[common.Address][]byte{behaviorCaller: precompileCallProgram(vm.STATICCALL, behaviorBank, input)},
			to:         &behaviorCaller,
			usedGas:    985_051,
			markerAddr: behaviorCaller,
			wantMarker: common.BigToHash(big.NewInt(1)),
		},
		{
			name:       "DELEGATECALL",
			code:       map[common.Address][]byte{behaviorCaller: precompileCallProgram(vm.DELEGATECALL, behaviorBank, input)},
			to:         &behaviorCaller,
			usedGas:    985_051,
			markerAddr: behaviorCaller,
			wantMarker: common.BigToHash(big.NewInt(1)),
		},
		{
			name:       "CALLCODE",
			code:       map[common.Address][]byte{behaviorCaller: precompileCallProgram(vm.CALLCODE, behaviorBank, input)},
			to:         &behaviorCaller,
			usedGas:    985_051,
			markerAddr: behaviorCaller,
			wantMarker: common.BigToHash(big.NewInt(1)),
		},
		{
			name: "nested CALL through an intermediate frame",
			code: map[common.Address][]byte{
				behaviorOuter:  precompileCallProgram(vm.CALL, behaviorCaller, nil),
				behaviorCaller: precompileCallProgram(vm.CALL, behaviorBank, input),
			},
			to:         &behaviorOuter,
			usedGas:    985_090,
			markerAddr: behaviorCaller,
			wantMarker: common.BigToHash(big.NewInt(1)),
		},
		{
			name:       "CREATE tx constructor",
			to:         nil,
			data:       precompileCallProgram(vm.CALL, behaviorBank, input),
			usedGas:    985_568,
			markerAddr: createdByTx,
			wantMarker: common.BigToHash(big.NewInt(1)),
		},
		{
			name:       "CREATE opcode constructor",
			code:       map[common.Address][]byte{behaviorFactory: createProgram(vm.CREATE, precompileCallProgram(vm.CALL, behaviorBank, input))},
			to:         &behaviorFactory,
			usedGas:    985_205,
			markerAddr: crypto.CreateAddress(behaviorFactory, 1),
			wantMarker: common.BigToHash(big.NewInt(1)),
		},
		{
			name:       "CREATE2 opcode constructor",
			code:       map[common.Address][]byte{behaviorFactory: createProgram(vm.CREATE2, precompileCallProgram(vm.CALL, behaviorBank, input))},
			to:         &behaviorFactory,
			usedGas:    985_205,
			markerAddr: crypto.CreateAddress2(behaviorFactory, common.Hash{}, crypto.Keccak256(precompileCallProgram(vm.CALL, behaviorBank, input))),
			wantMarker: common.BigToHash(big.NewInt(1)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sdb := newBehaviorStateDB(t, tc.code)
			exec := newBehaviorExecutor(sdb)
			gp := core.NewGasPool(10_000_000)

			res, err := exec.ExecuteTransactionFeeCharged(behaviorTx(tc.to, tc.data), behaviorSender, big.NewInt(0), gp)

			// Abort surfaces unchanged as the top-level vm error at every depth.
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Same(t, gigaprecompiles.ErrInvalidPrecompileCall, res.Err)
			require.True(t, gigautils.ShouldExecutionAbort(res.Err))
			require.Equal(t, "invalid precompile call", res.Err.Error())
			require.Empty(t, res.ReturnData)
			require.Equal(t, tc.usedGas, res.UsedGas)

			// Executor bumps the sender nonce.
			require.Equal(t, uint64(1), sdb.GetNonce(behaviorSender))
			// No revert on abort: pre-call writes remain.
			require.Equal(t, tc.wantMarker, sdb.GetState(tc.markerAddr, slotMarker))
			// Success slot is never written.
			require.Equal(t, common.Hash{}, sdb.GetState(tc.markerAddr, slotSuccess))
		})
	}
}

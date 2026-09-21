package evmonly

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// plainTransferScenario is one block whose execution is compared between the
// plain-transfer path and core.ApplyMessage. wantFast is how many of its
// transactions the fast path must have applied, so a scenario cannot pass by
// falling through to ApplyMessage on both sides.
type plainTransferScenario struct {
	name     string
	cfg      Config
	setup    func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte)
	opts     []Option
	wantFast uint64
	wantErr  error
}

func plainTransferScenarios(t *testing.T) []plainTransferScenario {
	t.Helper()
	chainID := big.NewInt(testChainID)
	funded := big.NewInt(1_000_000_000_000)
	recipient := testAddress(0xd1)
	newSender := func(t *testing.T, state *MemoryState, balance *big.Int) (*ecdsa.PrivateKey, common.Address) {
		t.Helper()
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		state.SetBalance(sender, balance)
		return key, sender
	}
	legacy := func(t *testing.T, key *ecdsa.PrivateKey, nonce uint64, to *common.Address, value *big.Int, gas uint64, gasPrice int64) []byte {
		t.Helper()
		return signLegacyTxWithGasPrice(t, key, chainID, nonce, to, value, nil, gas, big.NewInt(gasPrice))
	}
	dynamic := func(t *testing.T, key *ecdsa.PrivateKey, nonce uint64, to *common.Address, value *big.Int, tip, feeCap int64, gas uint64) []byte {
		t.Helper()
		return signDynamicFeeTxWithFees(t, key, chainID, nonce, to, value, nil, big.NewInt(tip), big.NewInt(feeCap), gas)
	}

	return []plainTransferScenario{
		{
			name: "legacy transfer to existing account",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				state.SetBalance(recipient, big.NewInt(5))
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(1234), 100_000, 3)}
			},
			wantFast: 1,
		},
		{
			name: "legacy transfer creates the recipient",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(1234), 100_000, 3)}
			},
			wantFast: 1,
		},
		{
			name: "zero value transfer to a nonexistent account",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(0), 21_000, 1)}
			},
			wantFast: 1,
		},
		{
			name: "dynamic fee transfer with base fee and tip",
			setup: func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte) {
				ctx.BaseFee = big.NewInt(7)
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{dynamic(t, key, 0, &recipient, big.NewInt(17), 10, 12, 100_000)}
			},
			wantFast: 1,
		},
		{
			name: "dynamic fee transfer where the tip exceeds fee cap minus base fee",
			setup: func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte) {
				ctx.BaseFee = big.NewInt(10)
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{dynamic(t, key, 0, &recipient, big.NewInt(17), 5, 12, 100_000)}
			},
			wantFast: 1,
		},
		{
			name: "self transfer",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, sender := newSender(t, state, funded)
				return state, [][]byte{legacy(t, key, 0, &sender, big.NewInt(999), 100_000, 2)}
			},
			wantFast: 1,
		},
		{
			name: "coinbase is the sender",
			setup: func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, sender := newSender(t, state, funded)
				ctx.Coinbase = sender
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(23), 100_000, 5)}
			},
			wantFast: 1,
		},
		{
			name: "coinbase is the recipient",
			setup: func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				ctx.Coinbase = recipient
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(23), 100_000, 5)}
			},
			wantFast: 1,
		},
		{
			name: "sender holds exactly gas limit times fee cap plus value",
			setup: func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte) {
				ctx.BaseFee = big.NewInt(2)
				state := NewMemoryState()
				exact := new(big.Int).Add(new(big.Int).Mul(big.NewInt(50_000), big.NewInt(9)), big.NewInt(77))
				key, _ := newSender(t, state, exact)
				return state, [][]byte{dynamic(t, key, 0, &recipient, big.NewInt(77), 1, 9, 50_000)}
			},
			wantFast: 1,
		},
		{
			name: "sender is one wei short",
			setup: func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte) {
				ctx.BaseFee = big.NewInt(2)
				state := NewMemoryState()
				short := new(big.Int).Add(new(big.Int).Mul(big.NewInt(50_000), big.NewInt(9)), big.NewInt(76))
				key, _ := newSender(t, state, short)
				return state, [][]byte{dynamic(t, key, 0, &recipient, big.NewInt(77), 1, 9, 50_000)}
			},
			wantErr: core.ErrInsufficientFunds,
		},
		{
			name: "nonce too high",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{legacy(t, key, 1, &recipient, big.NewInt(1), 100_000, 1)}
			},
			wantErr: core.ErrNonceTooHigh,
		},
		{
			name: "nonce too low",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, sender := newSender(t, state, funded)
				state.SetNonce(sender, 3)
				return state, [][]byte{legacy(t, key, 2, &recipient, big.NewInt(1), 100_000, 1)}
			},
			wantErr: core.ErrNonceTooLow,
		},
		{
			name: "nonce check disabled",
			cfg:  Config{DisableNonceCheck: true},
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, sender := newSender(t, state, funded)
				state.SetNonce(sender, 3)
				return state, [][]byte{legacy(t, key, 7, &recipient, big.NewInt(1), 100_000, 1)}
			},
			wantFast: 1,
		},
		{
			name: "gas limit one below intrinsic gas",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(1), params.TxGas-1, 1)}
			},
			wantErr: core.ErrIntrinsicGas,
		},
		{
			name: "fee cap below base fee",
			cfg:  Config{DisableGasPriceCheck: true},
			setup: func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte) {
				ctx.BaseFee = big.NewInt(5)
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{dynamic(t, key, 0, &recipient, big.NewInt(1), 1, 4, 100_000)}
			},
			wantErr: core.ErrFeeCapTooLow,
		},
		{
			name: "tip above fee cap",
			cfg:  Config{DisableGasPriceCheck: true},
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{dynamic(t, key, 0, &recipient, big.NewInt(1), 5, 4, 100_000)}
			},
			wantErr: core.ErrTipAboveFeeCap,
		},
		{
			name: "block gas limit exhausted by a transfer",
			setup: func(t *testing.T, ctx *BlockContext) (*MemoryState, [][]byte) {
				ctx.GasLimit = 30_000
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(1), 30_001, 1)}
			},
			wantErr: core.ErrGasLimitReached,
		},
		{
			name: "sender with code",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, sender := newSender(t, state, funded)
				state.SetCode(sender, []byte{0x00})
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(1), 100_000, 1)}
			},
			wantErr: core.ErrSenderNoEOA,
		},
		{
			name: "delegated sender",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, sender := newSender(t, state, funded)
				state.SetCode(sender, ethtypes.AddressToDelegation(testAddress(0xaa)))
				return state, [][]byte{legacy(t, key, 0, &recipient, big.NewInt(1), 100_000, 1)}
			},
			wantFast: 1,
		},
		{
			name: "rejected pre-checks become failed receipts",
			cfg:  Config{RejectUnappliableTxs: true},
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state, funded)
				poorKey, _ := newSender(t, state, big.NewInt(1))
				return state, [][]byte{
					legacy(t, key, 1, &recipient, big.NewInt(1), 100_000, 1),
					legacy(t, poorKey, 0, &recipient, big.NewInt(1), 100_000, 1),
					legacy(t, key, 0, &recipient, big.NewInt(1), 100_000, 1),
				}
			},
			wantFast: 1,
		},
		{
			name: "state fault aborts the block instead of rejecting the transaction",
			cfg:  Config{RejectUnappliableTxs: true},
			// The sender is absent from the store, so its balance comes from the
			// missing-account state, which reports one that exceeds 256 bits.
			opts: []Option{WithMissingAccountState(&overflowingBalanceState{MemoryState: NewMemoryState()})},
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				return NewMemoryState(), [][]byte{legacy(t, key, 0, &recipient, big.NewInt(1), 100_000, 1)}
			},
			wantErr: errStateBalanceOverflow,
		},
		{
			name: "chained transfers in one block",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				keyA, _ := newSender(t, state, funded)
				keyB, senderB := newSender(t, state, big.NewInt(0))
				keyC, senderC := newSender(t, state, big.NewInt(0))
				return state, [][]byte{
					legacy(t, keyA, 0, &senderB, big.NewInt(100_000), 100_000, 1),
					legacy(t, keyB, 0, &senderC, big.NewInt(50_000), 21_000, 1),
					legacy(t, keyC, 0, &recipient, big.NewInt(1), 21_000, 1),
					legacy(t, keyA, 1, &recipient, big.NewInt(1), 100_000, 1),
				}
			},
			wantFast: 4,
		},
	}
}

// plainTransferFallthroughScenarios are transactions the fast path must leave to
// core.ApplyMessage.
func plainTransferFallthroughScenarios(t *testing.T) []plainTransferScenario {
	t.Helper()
	chainID := big.NewInt(testChainID)
	funded := big.NewInt(1_000_000_000_000)
	newSender := func(t *testing.T, state *MemoryState) (*ecdsa.PrivateKey, common.Address) {
		t.Helper()
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		state.SetBalance(sender, funded)
		return key, sender
	}
	return []plainTransferScenario{
		{
			name: "recipient with code",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state)
				contract := testAddress(0xc0)
				state.SetCode(contract, []byte{0x00})
				return state, [][]byte{signLegacyTxWithGasPrice(t, key, chainID, 0, &contract, big.NewInt(1), nil, 100_000, big.NewInt(1))}
			},
		},
		{
			name: "delegated recipient",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state)
				delegated := testAddress(0xc1)
				state.SetCode(delegated, ethtypes.AddressToDelegation(testAddress(0xc2)))
				return state, [][]byte{signLegacyTxWithGasPrice(t, key, chainID, 0, &delegated, big.NewInt(1), nil, 100_000, big.NewInt(1))}
			},
		},
		{
			name: "precompile recipient",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state)
				identity := testAddress(0x04)
				return state, [][]byte{signLegacyTxWithGasPrice(t, key, chainID, 0, &identity, big.NewInt(1), nil, 100_000, big.NewInt(1))}
			},
		},
		{
			name: "calldata to a codeless account",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state)
				to := testAddress(0xc3)
				return state, [][]byte{signLegacyTxWithGasPrice(t, key, chainID, 0, &to, big.NewInt(1), []byte{0x01}, 100_000, big.NewInt(1))}
			},
		},
		{
			name: "access list to a codeless account",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state)
				to := testAddress(0xc4)
				accessList := ethtypes.AccessList{{Address: to, StorageKeys: []common.Hash{testHash(0x01)}}}
				return state, [][]byte{signAccessListTx(t, key, chainID, 0, &to, big.NewInt(1), nil, 120_000, big.NewInt(1), accessList)}
			},
		},
		{
			name: "authorization list to a codeless account",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state)
				authKey, err := crypto.GenerateKey()
				require.NoError(t, err)
				to := testAddress(0xc5)
				auth, err := ethtypes.SignSetCode(authKey, ethtypes.SetCodeAuthorization{
					ChainID: *uint256.MustFromBig(chainID),
					Address: testAddress(0xc6),
					Nonce:   0,
				})
				require.NoError(t, err)
				tx := ethtypes.NewTx(&ethtypes.SetCodeTx{
					ChainID:   uint256.MustFromBig(chainID),
					Nonce:     0,
					GasTipCap: uint256.NewInt(1),
					GasFeeCap: uint256.NewInt(1),
					Gas:       120_000,
					To:        to,
					Value:     uint256.NewInt(1),
					AuthList:  []ethtypes.SetCodeAuthorization{auth},
				})
				signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
				require.NoError(t, err)
				raw, err := signed.MarshalBinary()
				require.NoError(t, err)
				return state, [][]byte{raw}
			},
		},
		{
			name: "contract creation",
			setup: func(t *testing.T, _ *BlockContext) (*MemoryState, [][]byte) {
				state := NewMemoryState()
				key, _ := newSender(t, state)
				return state, [][]byte{signLegacyTxWithGasPrice(t, key, chainID, 0, nil, big.NewInt(0), []byte{0x00}, 100_000, big.NewInt(1))}
			},
		},
	}
}

// runPlainTransferBlock executes the scenario's block once with the fast path
// and once with it disabled, on the same initial state and with the same OCC
// setting, and returns both results.
func runPlainTransferBlock(t *testing.T, sc plainTransferScenario, occWorkers int) (fast, slow *BlockResult, fastCount uint64, fastErr, slowErr error) {
	t.Helper()
	ctx := blockContext(big.NewInt(testChainID))
	state, rawTxs := sc.setup(t, &ctx)
	run := func(disable bool) (*BlockResult, uint64, error) {
		cfg := sc.cfg
		if cfg.MinGasPrice == nil {
			cfg.MinGasPrice = big.NewInt(0)
		}
		cfg.OCCWorkers = occWorkers
		cfg.DisablePlainTransferFastPath = disable
		reader := bindTestExecutionMetrics(t)
		executor := NewExecutor(cfg, append([]Option{withTestState(state)}, sc.opts...)...)
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: ctx, Txs: rawTxs})
		return result, plainTransfersRecorded(t, reader), err
	}
	fast, fastCount, fastErr = run(false)
	slow, slowCount, slowErr := run(true)
	require.Zero(t, slowCount, "ApplyMessage run must not take the fast path")
	return fast, slow, fastCount, fastErr, slowErr
}

// plainTransfersRecorded returns the giga_evmonly_plain_transfers_total count
// collected so far; a block that failed records nothing.
func plainTransfersRecorded(t *testing.T, reader *sdkmetric.ManualReader) uint64 {
	t.Helper()
	collected := collectOCCMetrics(t, reader)
	if _, ok := collected["giga_evmonly_plain_transfers_total"]; !ok {
		return 0
	}
	return uint64(requireCounter(t, collected, "giga_evmonly_plain_transfers_total")) //nolint:gosec // counter is non-negative
}

func requirePlainTransferParity(t *testing.T, sc plainTransferScenario, fast, slow *BlockResult, fastErr, slowErr error) {
	t.Helper()
	if sc.wantErr != nil {
		require.ErrorIs(t, slowErr, sc.wantErr)
		require.ErrorIs(t, fastErr, sc.wantErr)
		require.Equal(t, slowErr.Error(), fastErr.Error())
		return
	}
	require.NoError(t, slowErr)
	require.NoError(t, fastErr)
	require.Equal(t, slow.GasUsed, fast.GasUsed)
	require.Equal(t, slow.Txs, fast.Txs)
	require.Equal(t, slow.Receipts, fast.Receipts)
	require.Equal(t, slow.ChangeSet, fast.ChangeSet)
}

func TestPlainTransferMatchesApplyMessage(t *testing.T) {
	for _, occWorkers := range []int{0, 4} {
		for _, sc := range plainTransferScenarios(t) {
			t.Run(sc.name, func(t *testing.T) {
				fast, slow, fastCount, fastErr, slowErr := runPlainTransferBlock(t, sc, occWorkers)
				requirePlainTransferParity(t, sc, fast, slow, fastErr, slowErr)
				if sc.wantErr == nil {
					require.Equal(t, sc.wantFast, fastCount, "transactions applied by the fast path")
				}
			})
		}
	}
}

func TestPlainTransferFallsThroughToApplyMessage(t *testing.T) {
	for _, sc := range plainTransferFallthroughScenarios(t) {
		t.Run(sc.name, func(t *testing.T) {
			fast, slow, fastCount, fastErr, slowErr := runPlainTransferBlock(t, sc, 0)
			requirePlainTransferParity(t, sc, fast, slow, fastErr, slowErr)
			require.Zero(t, fastCount, "fast path must not apply this transaction")
		})
	}
}

// TestPlainTransferAccessSetsMatchApplyMessage pins the OCC read and write sets a
// fast-path transfer records to those ApplyMessage records, so conflict
// detection sees the same footprint either way.
func TestPlainTransferAccessSetsMatchApplyMessage(t *testing.T) {
	chainID := big.NewInt(testChainID)
	ctx := blockContext(chainID)
	ctx.BaseFee = big.NewInt(3)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xd2)
	initial := NewMemoryState()
	initial.SetBalance(sender, big.NewInt(1_000_000_000_000))
	rawTx := signDynamicFeeTxWithFees(t, key, chainID, 0, &recipient, big.NewInt(17), nil, big.NewInt(2), big.NewInt(9), 100_000)

	accessSets := func(disable bool) (map[stateAccessKey]struct{}, map[stateAccessKey]struct{}) {
		executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), DisablePlainTransferFastPath: disable}, withTestState(initial))
		req, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: ctx, Txs: [][]byte{rawTx}})
		require.NoError(t, err)
		exec, err := executor.executeTxSpeculative(t.Context(), initial, req, 0, 0, executor.newBlockExecEnv(ctx, nil), ctx.BaseFee, ctx.GasLimit)
		require.NoError(t, err)
		require.NoError(t, exec.err)
		return exec.readSet, exec.writeSet
	}
	fastReads, fastWrites := accessSets(false)
	slowReads, slowWrites := accessSets(true)
	require.Equal(t, slowWrites, fastWrites)
	for key := range fastReads {
		require.Contains(t, slowReads, key)
	}
	require.Contains(t, fastReads, stateAccessKey{kind: stateAccessBalance, address: sender})
	require.Contains(t, fastReads, stateAccessKey{kind: stateAccessNonce, address: sender})
	require.Contains(t, fastReads, stateAccessKey{kind: stateAccessCode, address: recipient})
}

func BenchmarkPlainTransfer(b *testing.B) {
	for _, disable := range []bool{false, true} {
		name := "fast-path"
		if disable {
			name = "apply-message"
		}
		b.Run(name, func(b *testing.B) {
			chainID := big.NewInt(testChainID)
			ctx := blockContext(chainID)
			ctx.BaseFee = big.NewInt(1)
			key, err := crypto.GenerateKey()
			require.NoError(b, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			recipient := testAddress(0xd3)
			initial := NewMemoryState()
			initial.SetBalance(sender, big.NewInt(1_000_000_000_000))
			raw := signLegacyTxWithGasPrice(b, key, chainID, 0, &recipient, big.NewInt(1), nil, 21_000, big.NewInt(1))
			executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), DisablePlainTransferFastPath: disable}, withTestState(initial))
			req, err := executor.PrepareBlock(b.Context(), BlockRequest{Context: ctx, Txs: [][]byte{raw}})
			require.NoError(b, err)
			env := executor.newBlockExecEnv(ctx, nil)
			stateDB := executor.acquireStateDB(initial)
			defer executor.releaseStateDB(stateDB)
			evm := newTxEVM(env, stateDB)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				stateDB.reset(initial)
				gasPool := new(core.GasPool).AddGas(ctx.GasLimit)
				if _, _, err := executor.executeTx(evm, stateDB, gasPool, ctx, req.Txs[0], 0, 0, ctx.BaseFee); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

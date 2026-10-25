package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/tracers"
	tracerlogger "github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	traceChainID  = 713715
	traceGasLimit = 30_000_000
	// traceTransfer is the gas a plain value transfer uses.
	traceTransfer = 21_000
)

var (
	traceRecipient = common.HexToAddress("0x1000000000000000000000000000000000000001")
	traceBlockTime = time.Unix(1_700_000_000, 0)
)

// traceChain is a two-block chain served to the debug API: block 1 is empty, and block 2 holds two
// transfers from one sender around two spent-nonce transactions the block rejected: a duplicate,
// which stored no receipt, and one with a new hash, which stored a receipt that used no gas.
type traceChain struct {
	sender   common.Address
	blocks   map[int64]*coretypes.ResultBlock
	executed []*ethtypes.Transaction // block 2's transactions that executed, in order
	backend  *testBackend
	store    receipt.ReceiptStore
}

func newTraceChain(t *testing.T) *traceChain {
	t.Helper()
	first, rawFirst := testSignedTransactionWithNonce(t, 0)
	_, rawDuplicate := testSignedTransactionWithNonce(t, 0)
	stale, rawStale := dynamicFeeTransfer(t, 0)
	second, rawSecond := dynamicFeeTransfer(t, 1)
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(traceChainID)), first)
	require.NoError(t, err)
	chain := &traceChain{
		sender: sender,
		blocks: map[int64]*coretypes.ResultBlock{
			1: traceResultBlock(1, nil),
			2: traceResultBlock(2, tmtypes.Txs{rawFirst, rawDuplicate, rawStale, rawSecond}),
		},
		executed: []*ethtypes.Transaction{first, second},
		store:    evmonly.NewMemoryReceiptStore(),
	}
	stored := func(tx *ethtypes.Transaction, index uint32, gasUsed uint64) receipt.ReceiptRecord {
		return receipt.ReceiptRecord{TxHash: tx.Hash(), Receipt: &evmtypes.Receipt{
			TxHashHex:        tx.Hash().Hex(),
			BlockNumber:      2,
			TransactionIndex: index,
			From:             sender.Hex(),
			GasUsed:          gasUsed,
		}}
	}
	ctx := sdk.Context{}.WithContext(t.Context())
	require.NoError(t, chain.store.SetReceipts(ctx.WithBlockHeight(1), nil))
	require.NoError(t, chain.store.SetReceipts(ctx.WithBlockHeight(2), []receipt.ReceiptRecord{
		stored(first, 0, traceTransfer), stored(stale, 2, 0), stored(second, 3, traceTransfer),
	}))

	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = big.NewInt(traceChainID)
	executor := evmonly.NewExecutor(evmonly.Config{ChainConfig: &chainConfig, RejectUnappliableTxs: true})
	// The state after block 1, which block 2 executed against.
	base := evmonly.NewMemoryState()
	base.SetBalance(sender, big.NewInt(1_000_000_000_000_000_000))
	blockContext := func(header *ethtypes.Header) evmonly.BlockContext {
		return evmonly.BlockContext{
			Number:      header.Number.Uint64(),
			Time:        header.Time,
			GasLimit:    traceGasLimit,
			ChainID:     big.NewInt(traceChainID),
			BaseFee:     new(big.Int),
			BlobBaseFee: new(big.Int),
			ParentHash:  header.ParentHash,
		}
	}
	replay := func(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
		view := evmonly.NewMemoryStore(base).OpenView()
		stateDB, err := executor.ReplayState(ctx, view, evmonly.BlockRequest{Context: blockContext(header), Txs: txs})
		return stateDB, view.Close, err
	}
	chain.backend = &testBackend{
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			height := int64(2)
			if req.Height != nil {
				height = int64(*req.Height)
			}
			if height <= 0 {
				return nil, fmt.Errorf("%w: %d", coretypes.ErrZeroOrNegativeHeight, height)
			}
			block, ok := chain.blocks[height]
			if !ok {
				return nil, fmt.Errorf("%w: %d", coretypes.ErrHeightExceedsChainHead, height)
			}
			return block, nil
		},
		blockByHash: func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
			for _, block := range chain.blocks {
				if common.BytesToHash(block.BlockID.Hash) == common.BytesToHash(req.Hash) {
					return block, nil
				}
			}
			return nil, nil
		},
		blockNumber: func() uint64 { return 2 },
		gasLimit:    func() (uint64, error) { return traceGasLimit, nil },
		baseFee:     func() (*big.Int, error) { return new(big.Int), nil },
		chainConfig: func() (*params.ChainConfig, error) { return &chainConfig, nil },
		chainID:     func() uint64 { return traceChainID },
		blockContext: func(header *ethtypes.Header) (vm.BlockContext, error) {
			return evmonly.EVMBlockContext(blockContext(header)), nil
		},
		stateAt: func(ctx context.Context, height int64) (vm.StateDB, func(), error) {
			header := chain.header(2)
			switch height {
			case 0, 1:
				// Block 1 is empty, so it left the state block 0 did.
				return replay(ctx, header, nil)
			case 2:
				return replay(ctx, header, chain.blocks[2].Block.Txs.ToSliceOfBytes())
			}
			return nil, nil, fmt.Errorf("the EVM state after block %d is not held by this node", height)
		},
		stateBeforeTx: replay,
	}
	return chain
}

// dynamicFeeTransfer signs, with testSignedTransaction's key, a dynamic-fee transfer of 1 wei to
// traceRecipient whose fee cap is above its tip, so it pays its tip only at a zero base fee.
func dynamicFeeTransfer(t *testing.T, nonce uint64) (*ethtypes.Transaction, []byte) {
	t.Helper()
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   big.NewInt(traceChainID),
		Nonce:     nonce,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(5_000_000_000),
		Gas:       traceTransfer,
		To:        &traceRecipient,
		Value:     big.NewInt(1),
	}), ethtypes.LatestSignerForChainID(big.NewInt(traceChainID)), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	return tx, raw
}

// traceResultBlock returns the stored block at height, whose hash is its height.
func traceResultBlock(height int64, txs tmtypes.Txs) *coretypes.ResultBlock {
	return &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: traceBlockHash(height).Bytes()},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: height, Time: traceBlockTime.Add(time.Duration(height) * time.Second)},
			Data:   tmtypes.Data{Txs: txs},
		},
	}
}

func traceBlockHash(height int64) common.Hash {
	return common.BigToHash(big.NewInt(height))
}

// header returns the header the chain executed block height at.
func (c *traceChain) header(height int64) *ethtypes.Header {
	return &ethtypes.Header{
		Number:     big.NewInt(height),
		Time:       uint64(traceBlockTime.Add(time.Duration(height) * time.Second).Unix()), //nolint:gosec // G115: positive.
		ParentHash: traceBlockHash(height - 1),
	}
}

// client serves the chain's debug API with limits and returns a client for it.
func (c *traceChain) client(t *testing.T, limits gigaconfig.RPCConfig) *ethrpc.Client {
	t.Helper()
	handler, err := newHandler(c.backend, c.store, limits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

// callFrame is the part of a callTracer frame these tests read.
type callFrame struct {
	Type    string         `json:"type"`
	From    common.Address `json:"from"`
	To      common.Address `json:"to"`
	Value   *hexutil.Big   `json:"value"`
	GasUsed hexutil.Uint64 `json:"gasUsed"`
	Error   string         `json:"error"`
}

// prestateDiff is the part of a diff-mode prestateTracer result these tests read.
type prestateDiff struct {
	Pre  map[common.Address]prestateAccount `json:"pre"`
	Post map[common.Address]prestateAccount `json:"post"`
}

type prestateAccount struct {
	Balance *hexutil.Big `json:"balance"`
	Nonce   uint64       `json:"nonce"`
}

func tracer(name string) *tracers.TraceConfig {
	return &tracers.TraceConfig{Tracer: &name}
}

func TestDebugTraceTransaction(t *testing.T) {
	chain := newTraceChain(t)
	client := chain.client(t, noRPCLimits)
	second := chain.executed[1]

	t.Run("default struct logger", func(t *testing.T) {
		var got tracerlogger.ExecutionResult
		require.NoError(t, client.CallContext(t.Context(), &got, "debug_traceTransaction", second.Hash()))
		require.Equal(t, uint64(traceTransfer), got.Gas)
		require.False(t, got.Failed)
		require.Empty(t, got.StructLogs)
	})

	t.Run("callTracer", func(t *testing.T) {
		var got callFrame
		require.NoError(t, client.CallContext(t.Context(), &got, "debug_traceTransaction", second.Hash(), tracer("callTracer")))
		require.Equal(t, callFrame{
			Type: "CALL", From: chain.sender, To: traceRecipient, Value: (*hexutil.Big)(big.NewInt(1)), GasUsed: traceTransfer,
		}, got)
	})

	t.Run("prestateTracer runs on the state the block left before the transaction", func(t *testing.T) {
		config := tracer("prestateTracer")
		config.TracerConfig = json.RawMessage(`{"diffMode":true}`)
		var got prestateDiff
		require.NoError(t, client.CallContext(t.Context(), &got, "debug_traceTransaction", second.Hash(), config))
		// The first transfer ran before it and the rejected duplicate did not.
		require.Equal(t, uint64(1), got.Pre[chain.sender].Nonce)
		require.Equal(t, uint64(2), got.Post[chain.sender].Nonce)
		// The sender pays the value and its gas at its tip, the price it executed at with no base fee,
		// rather than at its fee cap.
		spent := new(big.Int).Sub(got.Pre[chain.sender].Balance.ToInt(), got.Post[chain.sender].Balance.ToInt())
		want := new(big.Int).Add(big.NewInt(1), new(big.Int).Mul(big.NewInt(traceTransfer), second.GasTipCap()))
		require.Equal(t, want, spent)
	})

	for _, tc := range []struct {
		name     string
		hash     common.Hash
		config   *tracers.TraceConfig
		wantCode int
	}{
		{name: "unknown transaction", hash: common.HexToHash("0xdead"), wantCode: -32602},
		{name: "JavaScript tracer", hash: second.Hash(), config: tracer("{result: function() { return 1 }}"), wantCode: -32602},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got json.RawMessage
			err := client.CallContext(t.Context(), &got, "debug_traceTransaction", tc.hash, tc.config)
			var rpcErr ethrpc.Error
			require.ErrorAs(t, err, &rpcErr)
			require.Equal(t, tc.wantCode, rpcErr.ErrorCode())
		})
	}
}

func TestDebugTraceBlock(t *testing.T) {
	chain := newTraceChain(t)
	client := chain.client(t, noRPCLimits)
	want := []common.Hash{chain.executed[0].Hash(), chain.executed[1].Hash()}

	for _, tc := range []struct {
		name   string
		method string
		arg    any
	}{
		{name: "by number", method: "debug_traceBlockByNumber", arg: ethrpc.BlockNumber(2)},
		{name: "latest", method: "debug_traceBlockByNumber", arg: ethrpc.LatestBlockNumber},
		{name: "by hash", method: "debug_traceBlockByHash", arg: traceBlockHash(2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []struct {
				TxHash common.Hash `json:"txHash"`
				Result callFrame   `json:"result"`
				Error  string      `json:"error"`
			}
			require.NoError(t, client.CallContext(t.Context(), &got, tc.method, tc.arg, tracer("callTracer")))
			hashes := make([]common.Hash, len(got))
			for i, result := range got {
				hashes[i] = result.TxHash
				require.Empty(t, result.Error)
				require.Equal(t, hexutil.Uint64(traceTransfer), result.Result.GasUsed)
			}
			// Neither rejected transaction is traced, so the sender's nonce carries over intact.
			require.Equal(t, want, hashes)
		})
	}

	t.Run("block 1, whose parent block 0 is not stored", func(t *testing.T) {
		var got []json.RawMessage
		require.NoError(t, client.CallContext(t.Context(), &got, "debug_traceBlockByNumber", ethrpc.BlockNumber(1), tracer("callTracer")))
		require.Empty(t, got)
	})

	t.Run("block whose receipts are not indexed", func(t *testing.T) {
		chain.backend.blockNumber = func() uint64 { return 3 }
		chain.blocks[3] = traceResultBlock(3, nil)
		defer delete(chain.blocks, 3)
		var got json.RawMessage
		err := client.CallContext(t.Context(), &got, "debug_traceBlockByNumber", ethrpc.BlockNumber(3), tracer("callTracer"))
		require.ErrorContains(t, err, errTraceBlockNotIndexed.Error())
	})
}

func TestDebugTraceCall(t *testing.T) {
	chain := newTraceChain(t)
	client := chain.client(t, noRPCLimits)
	transfer := map[string]any{"from": chain.sender, "to": traceRecipient, "value": "0x1"}

	t.Run("default struct logger at latest", func(t *testing.T) {
		var got tracerlogger.ExecutionResult
		require.NoError(t, client.CallContext(t.Context(), &got, "debug_traceCall", transfer, "latest"))
		require.Equal(t, uint64(traceTransfer), got.Gas)
		require.False(t, got.Failed)
		require.Equal(t, hexutil.Bytes{}, got.ReturnValue)
		require.Empty(t, got.StructLogs)
	})

	for _, tc := range []struct {
		name      string
		block     any
		txIndex   *hexutil.Uint
		wantNonce uint64
	}{
		{name: "latest reads the state after the latest block", block: "latest", wantNonce: 2},
		{name: "a numbered block reads the state after it", block: "0x2", wantNonce: 2},
		{name: "a block hash reads the state after it", block: traceBlockHash(2), wantNonce: 2},
		{name: "the block before reads its state", block: "0x1", wantNonce: 0},
		{name: "block 0 reads the state the chain started from", block: "0x0", wantNonce: 0},
		{name: "txIndex reads the state before that transaction", block: "0x2", txIndex: utilsPtr(hexutil.Uint(2)), wantNonce: 1},
		{name: "txIndex 0 of an empty block reads the state it started from", block: "0x1", txIndex: utilsPtr(hexutil.Uint(0)), wantNonce: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := map[string]any{"tracer": "prestateTracer"}
			if tc.txIndex != nil {
				config["txIndex"] = tc.txIndex
			}
			var got map[common.Address]prestateAccount
			require.NoError(t, client.CallContext(t.Context(), &got, "debug_traceCall", transfer, tc.block, config))
			require.Equal(t, tc.wantNonce, got[chain.sender].Nonce)
		})
	}

	t.Run("latest reads the newest block before its receipts are indexed", func(t *testing.T) {
		// Like a validator, the node holds only the newest block's state, and block 3's receipts are
		// still being written.
		chain.blocks[3] = traceResultBlock(3, nil)
		chain.backend.blockNumber = func() uint64 { return 3 }
		stateAt := chain.backend.stateAt
		chain.backend.stateAt = func(ctx context.Context, height int64) (vm.StateDB, func(), error) {
			if height != 3 {
				return nil, nil, fmt.Errorf("the EVM state after block %d is not held by this node", height)
			}
			return stateAt(ctx, 2)
		}
		defer func() {
			delete(chain.blocks, 3)
			chain.backend.blockNumber = func() uint64 { return 2 }
			chain.backend.stateAt = stateAt
		}()
		var got map[common.Address]prestateAccount
		require.NoError(t, client.CallContext(t.Context(), &got, "debug_traceCall", transfer, "latest", map[string]any{"tracer": "prestateTracer"}))
		require.Equal(t, uint64(2), got[chain.sender].Nonce)
	})

	t.Run("overrides are refused", func(t *testing.T) {
		config := map[string]any{"stateOverrides": map[string]any{chain.sender.Hex(): map[string]any{"nonce": "0x9"}}}
		var got json.RawMessage
		err := client.CallContext(t.Context(), &got, "debug_traceCall", transfer, "latest", config)
		var rpcErr ethrpc.Error
		require.ErrorAs(t, err, &rpcErr)
		require.Equal(t, -32602, rpcErr.ErrorCode())
	})
}

func utilsPtr[T any](v T) *T {
	return &v
}

func TestDebugTraceConcurrencyLimit(t *testing.T) {
	api := newDebugAPI(&testBackend{}, evmonly.NewMemoryReceiptStore(), gigaconfig.RPCConfig{MaxConcurrentTraceCalls: 1})

	_, done, err := api.beginTrace(t.Context())
	require.NoError(t, err)
	_, _, err = api.beginTrace(t.Context())
	require.ErrorIs(t, err, errTraceBusy)
	done()
	_, done, err = api.beginTrace(t.Context())
	require.NoError(t, err)
	done()

	unlimited := newDebugAPI(&testBackend{}, evmonly.NewMemoryReceiptStore(), gigaconfig.RPCConfig{})
	for range 3 {
		_, _, err := unlimited.beginTrace(t.Context())
		require.NoError(t, err)
	}
}

func TestDebugTraceTimeout(t *testing.T) {
	for _, tc := range []struct {
		name         string
		timeout      time.Duration
		wantDeadline bool
	}{
		{name: "configured timeout bounds the trace", timeout: time.Minute, wantDeadline: true},
		{name: "zero disables the timeout", timeout: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newDebugAPI(&testBackend{}, evmonly.NewMemoryReceiptStore(), gigaconfig.RPCConfig{TraceTimeout: tc.timeout})
			ctx, done, err := api.beginTrace(t.Context())
			require.NoError(t, err)
			defer done()
			_, hasDeadline := ctx.Deadline()
			require.Equal(t, tc.wantDeadline, hasDeadline)
		})
	}

	// An expired trace reports the deadline, not whatever partial result it returned.
	api := newDebugAPI(&testBackend{}, evmonly.NewMemoryReceiptStore(), gigaconfig.RPCConfig{TraceTimeout: time.Minute})
	ctx, cancel := context.WithCancel(t.Context())
	result, err := api.trace(ctx, nil, func(context.Context, *tracers.TraceConfig) (any, error) {
		cancel()
		return "partial", nil
	})
	require.Nil(t, result)
	require.ErrorIs(t, err, context.Canceled)
}

func TestValidateTracer(t *testing.T) {
	for _, tc := range []struct {
		name         string
		tracer       *string
		tracerConfig string
		wantErr      error
		wantName     string
	}{
		{name: "default struct logger"},
		{name: "native tracer", tracer: utilsPtr("callTracer"), wantName: "callTracer"},
		{name: "padded native tracer is trimmed", tracer: utilsPtr(" callTracer "), wantName: "callTracer"},
		{name: "empty name", tracer: utilsPtr(" "), wantErr: errTracerNameEmpty},
		{name: "JavaScript tracer", tracer: utilsPtr("{result: function() {}}"), wantErr: errTracerNotAllowed},
		{name: "muxTracer of native tracers", tracer: utilsPtr("muxTracer"), tracerConfig: `{"callTracer":{},"4byteTracer":{}}`, wantName: "muxTracer"},
		{name: "muxTracer nesting a JavaScript tracer", tracer: utilsPtr("muxTracer"), tracerConfig: `{"{}":{}}`, wantErr: errTracerNotAllowed},
		{name: "muxTracer nesting a padded name", tracer: utilsPtr("muxTracer"), tracerConfig: `{" callTracer":{}}`, wantErr: errNestedTracerPadding},
		{name: "muxTracer nested too deeply", tracer: utilsPtr("muxTracer"), tracerConfig: nestedMuxConfig(maxMuxTracerNestingDepth + 1), wantErr: errMuxTracerTooDeep},
		{name: "muxTracer nested to the limit", tracer: utilsPtr("muxTracer"), tracerConfig: nestedMuxConfig(maxMuxTracerNestingDepth), wantName: "muxTracer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &tracers.TraceConfig{Tracer: tc.tracer, TracerConfig: json.RawMessage(tc.tracerConfig)}
			err := validateTracer(config)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			if tc.wantName != "" {
				require.Equal(t, tc.wantName, *config.Tracer)
			}
		})
	}
}

// nestedMuxConfig returns a muxTracer config holding muxTracers depth levels deep, counting the
// config itself as the first.
func nestedMuxConfig(depth int) string {
	config := `{"callTracer":{}}`
	for range depth - 1 {
		config = fmt.Sprintf(`{"muxTracer":%s}`, config)
	}
	return config
}

func TestPrepareTraceConfigCapsTheStructLogger(t *testing.T) {
	const capBytes = 1024
	for _, tc := range []struct {
		name      string
		config    *tracers.TraceConfig
		maxBytes  int
		wantLimit int
	}{
		{name: "unset limit takes the cap", config: &tracers.TraceConfig{}, maxBytes: capBytes, wantLimit: capBytes},
		{name: "larger limit is lowered", config: &tracers.TraceConfig{Config: &tracerlogger.Config{Limit: 2 * capBytes}}, maxBytes: capBytes, wantLimit: capBytes},
		{name: "smaller limit is kept", config: &tracers.TraceConfig{Config: &tracerlogger.Config{Limit: capBytes / 2}}, maxBytes: capBytes, wantLimit: capBytes / 2},
		{name: "zero cap leaves the limit", config: &tracers.TraceConfig{Config: &tracerlogger.Config{Limit: 7}}, wantLimit: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newDebugAPI(&testBackend{}, evmonly.NewMemoryReceiptStore(), gigaconfig.RPCConfig{MaxTraceStructLogBytes: tc.maxBytes})
			require.NoError(t, api.prepareTraceConfig(tc.config))
			require.Equal(t, tc.wantLimit, tc.config.Limit)
		})
	}

	// A named tracer keeps no struct log, so its config is left alone.
	config := tracer("callTracer")
	api := newDebugAPI(&testBackend{}, evmonly.NewMemoryReceiptStore(), gigaconfig.RPCConfig{MaxTraceStructLogBytes: capBytes})
	require.NoError(t, api.prepareTraceConfig(config))
	require.Nil(t, config.Config)
}

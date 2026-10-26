package rpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// recordTraceGolden rewrites the trace golden files from this build instead of verifying against
// them. Off by default, and refused on CI.
var recordTraceGolden = flag.Bool("trace-golden-record", false,
	"rewrite the debug_trace* golden files from this build instead of verifying against them")

// traceGoldenDir holds one golden file per traced fixture, named after the fixture.
const traceGoldenDir = "testdata/trace"

// traceFixtures names the parity fixtures whose transactions, blocks and calls are traced.
var traceFixtures = []string{"synthetic-basic"}

// traceConfigs are the tracer configurations every trace runs with, each with the shape its result
// must have. golden, when set, reduces a result to what its golden entry holds.
var traceConfigs = []struct {
	name   string
	config string
	shape  func(t *testing.T, result json.RawMessage)
	golden func(t *testing.T, result json.RawMessage) json.RawMessage
}{
	{name: "structLogger", config: `{}`, shape: requireKeys("gas", "failed", "returnValue", "structLogs"), golden: summarizeStructLog},
	{name: "structLoggerMemory", config: `{"enableMemory":true,"enableReturnData":true}`, shape: requireKeys("gas", "failed", "returnValue", "structLogs"), golden: summarizeStructLog},
	{name: "callTracer", config: `{"tracer":"callTracer","tracerConfig":{"withLog":true}}`, shape: requireCallFrame},
	{name: "flatCallTracer", config: `{"tracer":"flatCallTracer"}`, shape: requireFlatCalls},
	{name: "prestateTracer", config: `{"tracer":"prestateTracer"}`, shape: requireObject},
	{name: "prestateTracerDiff", config: `{"tracer":"prestateTracer","tracerConfig":{"diffMode":true}}`, shape: requireKeys("pre", "post")},
	{name: "4byteTracer", config: `{"tracer":"4byteTracer"}`, shape: requireObject},
	{name: "muxTracer", config: `{"tracer":"muxTracer","tracerConfig":{"callTracer":{},"prestateTracer":{"diffMode":true}}}`, shape: requireKeys("callTracer", "prestateTracer")},
	{name: "noopTracer", config: `{"tracer":"noopTracer"}`, shape: requireObject},
}

// traceGoldenEntry is one traced request and its result under each of traceConfigs.
type traceGoldenEntry struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
	// Options are added to every tracer configuration the request runs with.
	Options json.RawMessage            `json:"options,omitempty"`
	Traces  map[string]json.RawMessage `json:"traces"`
}

// TestDebugTraceGolden traces every transaction and block of each fixture in traceFixtures, and the
// calls it records, with every tracer configuration. Each trace must succeed with its tracer's
// shape and equal the fixture's golden output.
func TestDebugTraceGolden(t *testing.T) {
	for _, name := range traceFixtures {
		t.Run(name, func(t *testing.T) {
			fixture, err := parity.LoadFixture(filepath.Join(parityFixtureDir, name+".json"))
			require.NoError(t, err)
			client := replayForTrace(t, fixture).client(t)
			got := traceRequests(t, fixture)
			for i := range got {
				traceEntry(t, client, &got[i])
			}
			path := filepath.Join(traceGoldenDir, name+".json")
			if *recordTraceGolden {
				writeTraceGolden(t, path, got)
				return
			}
			requireTraceGolden(t, path, got)
		})
	}
}

// traceRequests returns the requests traced for fixture: each transaction, each block, each call
// the fixture records at the latest block, and the first of those calls before the second
// transaction of the fixture's second block.
func traceRequests(t *testing.T, fixture *parity.Fixture) []traceGoldenEntry {
	t.Helper()
	var entries []traceGoldenEntry
	for _, block := range fixture.Blocks {
		for _, raw := range block.Txs {
			var tx ethtypes.Transaction
			require.NoError(t, tx.UnmarshalBinary(raw))
			entries = append(entries, traceGoldenEntry{Method: "debug_traceTransaction", Params: rawParams(t, tx.Hash())})
		}
		entries = append(entries, traceGoldenEntry{Method: "debug_traceBlockByNumber", Params: rawParams(t, block.Header.Number)})
	}
	var calls [][]json.RawMessage
	for _, request := range fixture.Requests {
		if request.Method == "eth_call" {
			calls = append(calls, request.Params)
		}
	}
	require.NotEmpty(t, calls, "fixture %s records no eth_call", fixture.Name)
	for _, params := range calls {
		entries = append(entries, traceGoldenEntry{Method: "debug_traceCall", Params: params})
	}
	require.GreaterOrEqual(t, len(fixture.Blocks), 2)
	entries = append(entries, traceGoldenEntry{
		Method:  "debug_traceCall",
		Params:  []json.RawMessage{calls[0][0], rawParams(t, fixture.Blocks[1].Header.Number)[0]},
		Options: json.RawMessage(`{"txIndex":"0x1"}`),
	})
	return entries
}

// traceEntry traces entry's request under every tracer configuration, requiring each to succeed with
// its tracer's shape, and records the results in entry.
func traceEntry(t *testing.T, client *ethrpc.Client, entry *traceGoldenEntry) {
	t.Helper()
	entry.Traces = map[string]json.RawMessage{}
	for _, tc := range traceConfigs {
		config := withOptions(t, tc.config, entry.Options)
		args := make([]any, 0, len(entry.Params)+1)
		for _, param := range entry.Params {
			args = append(args, param)
		}
		var result json.RawMessage
		require.NoError(t, client.CallContext(t.Context(), &result, entry.Method, append(args, config)...),
			"%s%s %s", entry.Method, parity.FormatParams(entry.Params), tc.name)
		if entry.Method == "debug_traceBlockByNumber" {
			requireBlockTraces(t, result, tc.shape)
		} else {
			tc.shape(t, result)
		}
		if tc.golden != nil {
			result = mapTxResults(t, entry.Method, result, tc.golden)
		}
		entry.Traces[tc.name] = result
	}
}

// mapTxResults returns result with fn applied to it, or for a block trace to each transaction's
// result.
func mapTxResults(t *testing.T, method string, result json.RawMessage, fn func(*testing.T, json.RawMessage) json.RawMessage) json.RawMessage {
	t.Helper()
	if method != "debug_traceBlockByNumber" {
		return fn(t, result)
	}
	var traces []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &traces))
	for _, trace := range traces {
		trace["result"] = fn(t, trace["result"])
	}
	mapped, err := json.Marshal(traces)
	require.NoError(t, err)
	return mapped
}

// summarizeStructLog returns a struct log with its steps replaced by their number and SHA-256, which
// keeps its golden entry small while any change to a step still changes the entry.
func summarizeStructLog(t *testing.T, result json.RawMessage) json.RawMessage {
	t.Helper()
	var structLog map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &structLog))
	var steps []json.RawMessage
	require.NoError(t, json.Unmarshal(structLog["structLogs"], &steps))
	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, structLog["structLogs"]))
	digest := sha256.Sum256(compact.Bytes())
	delete(structLog, "structLogs")
	structLog["structLogCount"] = rawParams(t, len(steps))[0]
	structLog["structLogsSHA256"] = rawParams(t, hex.EncodeToString(digest[:]))[0]
	summary, err := json.Marshal(structLog)
	require.NoError(t, err)
	return summary
}

// withOptions returns config with options' fields added.
func withOptions(t *testing.T, config string, options json.RawMessage) json.RawMessage {
	t.Helper()
	if len(options) == 0 {
		return json.RawMessage(config)
	}
	fields := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal([]byte(config), &fields))
	require.NoError(t, json.Unmarshal(options, &fields))
	merged, err := json.Marshal(fields)
	require.NoError(t, err)
	return merged
}

func writeTraceGolden(t *testing.T, path string, entries []traceGoldenEntry) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatal("refusing to re-record the trace golden files on CI: " +
			"the recorded traces are the expected traces, so a build that rewrites them verifies nothing")
	}
	content, err := json.MarshalIndent(entries, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, append(content, '\n'), 0o644))
	t.Logf("recorded %d traced requests into %s; review the diff before committing", len(entries), path)
}

// requireTraceGolden requires got to trace the same requests as the golden file at path, each with
// the same results.
func requireTraceGolden(t *testing.T, path string, got []traceGoldenEntry) {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err, "record it with -trace-golden-record")
	var want []traceGoldenEntry
	require.NoError(t, json.Unmarshal(content, &want))
	require.Len(t, got, len(want), "traced requests differ from %s", path)
	for i := range want {
		request := want[i].Method + parity.FormatParams(want[i].Params)
		require.Equal(t, request, got[i].Method+parity.FormatParams(got[i].Params))
		if len(want[i].Options) > 0 || len(got[i].Options) > 0 {
			require.JSONEq(t, string(want[i].Options), string(got[i].Options), request)
		}
		require.Len(t, got[i].Traces, len(want[i].Traces), request)
		for name, trace := range want[i].Traces {
			require.JSONEq(t, string(trace), string(got[i].Traces[name]), "%s %s", request, name)
		}
	}
}

// requireBlockTraces requires result to be a block trace in which every transaction traced without
// error and with shape.
func requireBlockTraces(t *testing.T, result json.RawMessage, shape func(*testing.T, json.RawMessage)) {
	t.Helper()
	var traces []struct {
		TxHash common.Hash     `json:"txHash"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	require.NoError(t, json.Unmarshal(result, &traces))
	for _, trace := range traces {
		require.Empty(t, trace.Error, "tx %s", trace.TxHash)
		require.NotEqual(t, common.Hash{}, trace.TxHash)
		shape(t, trace.Result)
	}
}

func requireCallFrame(t *testing.T, result json.RawMessage) {
	t.Helper()
	var frame struct {
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(result, &frame))
	require.Contains(t, []string{"CALL", "CREATE", "CREATE2"}, frame.Type, string(result))
	requireKeys("from", "gas", "gasUsed")(t, result)
}

func requireFlatCalls(t *testing.T, result json.RawMessage) {
	t.Helper()
	var calls []json.RawMessage
	require.NoError(t, json.Unmarshal(result, &calls))
	require.NotEmpty(t, calls)
	for _, call := range calls {
		requireKeys("action", "type", "blockHash", "blockNumber", "transactionHash", "transactionPosition", "traceAddress", "subtraces")(t, call)
	}
}

func requireObject(t *testing.T, result json.RawMessage) {
	t.Helper()
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &fields), string(result))
}

// requireKeys returns a check that a result is a JSON object holding every one of keys.
func requireKeys(keys ...string) func(*testing.T, json.RawMessage) {
	return func(t *testing.T, result json.RawMessage) {
		t.Helper()
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(result, &fields), string(result))
		for _, key := range keys {
			require.Contains(t, fields, key, string(result))
		}
	}
}

func rawParams(t *testing.T, params ...any) []json.RawMessage {
	t.Helper()
	raw := make([]json.RawMessage, len(params))
	for i, param := range params {
		encoded, err := json.Marshal(param)
		require.NoError(t, err)
		raw[i] = encoded
	}
	return raw
}

// traceReplayBackend serves a fixture replay to the debug API: the state and block context each
// replayed block executed with, and an empty block before the fixture's first, which a trace of
// that block reads as its parent.
type traceReplayBackend struct {
	*replayBackend
	contexts map[uint64]evmonly.BlockContext
	parent   *coretypes.ResultBlock
}

// replayForTrace executes an empty block and then fixture's blocks from its prestate, as an EVM-only
// node executes them: at a zero base fee with a zero coinbase.
func replayForTrace(t *testing.T, fixture *parity.Fixture) *traceReplayBackend {
	t.Helper()
	chainID := uint64(fixture.ChainID)
	store := evmonly.NewMemoryStore(prestateReader(fixture.Prestate))
	receipts := evmonly.NewMemoryReceiptStore()
	chainConfig := evmtypes.DefaultChainConfig().EthereumConfig(new(big.Int).SetUint64(chainID))
	executor := evmonly.NewExecutor(evmonly.Config{
		ChainConfig:          chainConfig,
		MinGasPrice:          new(big.Int),
		OCCWorkers:           replayOCCWorkers,
		RejectUnappliableTxs: true,
	}, evmonly.WithStore(store, store.EncodeChangeSet), evmonly.WithReceiptStore(receipts))
	t.Cleanup(executor.Close)

	first := fixture.Blocks[0]
	parentCtx := evmOnlyReplayContext(chainID, first)
	parentCtx.Number--
	parentCtx.Time--
	parentCtx.ParentHash = common.Hash{}
	parentCtx.BlockHash = first.Header.ParentHash
	backend := &traceReplayBackend{
		replayBackend: &replayBackend{
			chainID:     chainID,
			chainConfig: chainConfig,
			executor:    executor,
			store:       store,
			receipts:    receipts,
			byHash:      map[common.Hash]*coretypes.ResultBlock{},
		},
		contexts: map[uint64]evmonly.BlockContext{parentCtx.Number: parentCtx},
		parent: &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: first.Header.ParentHash.Bytes()},
			Block: &tmtypes.Block{Header: tmtypes.Header{
				Height: int64(parentCtx.Number),                   //nolint:gosec // fixture heights fit int64.
				Time:   first.Header.Time.Add(-time.Second).UTC(), // one second before the first block, as parentCtx
			}},
		},
	}
	execute := func(blockCtx evmonly.BlockContext, txs [][]byte) {
		result, err := executor.ExecuteBlock(t.Context(), evmonly.BlockRequest{Context: blockCtx, Txs: txs})
		require.NoError(t, err, "execute block %d", blockCtx.Number)
		result.Release()
	}
	execute(parentCtx, nil)
	for _, block := range fixture.Blocks {
		blockCtx := evmOnlyReplayContext(chainID, block)
		execute(blockCtx, rawTxs(block))
		resultBlock := consensusBlock(block)
		backend.blocks = append(backend.blocks, resultBlock)
		backend.byHash[block.Header.Hash] = resultBlock
		backend.contexts[blockCtx.Number] = blockCtx
		backend.tip = blockCtx
	}
	require.NoError(t, executor.AwaitCommits())
	return backend
}

// evmOnlyReplayContext returns the context v2 executed block at, with the zero base fee and coinbase
// an EVM-only node executes every block with.
func evmOnlyReplayContext(chainID uint64, block parity.Block) evmonly.BlockContext {
	blockCtx := replayBlockContext(chainID, block)
	blockCtx.BaseFee = new(big.Int)
	blockCtx.Coinbase = common.Address{}
	return blockCtx
}

func (b *traceReplayBackend) client(t *testing.T) *ethrpc.Client {
	t.Helper()
	handler, err := newHandler(b, b.receipts, noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func (b *traceReplayBackend) Block(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
	if req.Height != nil && int64(*req.Height) == b.parent.Block.Height {
		return b.parent, nil
	}
	return b.replayBackend.Block(ctx, req)
}

func (b *traceReplayBackend) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	blockCtx, err := b.contextFor(header)
	if err != nil {
		return vm.BlockContext{}, err
	}
	return evmonly.EVMBlockContext(blockCtx), nil
}

func (b *traceReplayBackend) EvmState(ctx context.Context) (vm.StateDB, func(), error) {
	return b.replayState(ctx, int64(b.EvmBlockNumber()), evmonly.BlockRequest{}) //nolint:gosec // G115: a replayed height fits in int64.
}

func (b *traceReplayBackend) EvmStateAt(ctx context.Context, height int64) (vm.StateDB, func(), error) {
	return b.replayState(ctx, height, evmonly.BlockRequest{})
}

func (b *traceReplayBackend) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
	blockCtx, err := b.contextFor(header)
	if err != nil {
		return nil, nil, err
	}
	return b.replayState(ctx, header.Number.Int64()-1, evmonly.BlockRequest{Context: blockCtx, Txs: txs})
}

// contextFor returns the context header's block executed with. It refuses a header whose fields an
// EVM-only node builds the context from differ from that block's.
func (b *traceReplayBackend) contextFor(header *ethtypes.Header) (evmonly.BlockContext, error) {
	blockCtx, ok := b.contexts[header.Number.Uint64()]
	if !ok {
		return evmonly.BlockContext{}, fmt.Errorf("block %s was not replayed", header.Number)
	}
	if header.Time != blockCtx.Time || header.ParentHash != blockCtx.ParentHash || header.BaseFee.Cmp(blockCtx.BaseFee) != 0 {
		return evmonly.BlockContext{}, fmt.Errorf("header of block %s (time %d, parent %s, base fee %s) differs from the block replayed (time %d, parent %s, base fee %s)",
			header.Number, header.Time, header.ParentHash, header.BaseFee, blockCtx.Time, blockCtx.ParentHash, blockCtx.BaseFee)
	}
	return blockCtx, nil
}

// replayState opens the replayed state after height and applies req's transactions to it.
func (b *traceReplayBackend) replayState(ctx context.Context, height int64, req evmonly.BlockRequest) (vm.StateDB, func(), error) {
	view, err := b.stateAt(height)
	if err != nil {
		return nil, nil, err
	}
	stateDB, err := b.executor.ReplayState(ctx, view, req)
	if err != nil {
		view.Close()
		return nil, nil, err
	}
	return stateDB, view.Close, nil
}

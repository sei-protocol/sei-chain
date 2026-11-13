package tests

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/stretchr/testify/require"
)

// Block 242615775 on pacific-1, recorded with its full set of 40 validators. Its first
// transaction's trace depends on the validator set replayed in BeginBlock.
const (
	fullSetBlock   = uint64(242615775)
	fullSetTxHash  = "0x1b4cfc91a923bfb9be3ae7e58e7e73311dc48ed8ab0d0b1a21b7d486c8474bf8"
	fullSetVersion = "v6.7"
	// fullSetOnChainLog is the data of the one log the transaction emitted on chain.
	fullSetOnChainLog = "0x00000000000000000000000000000000000000025f3e82a1609948226f21517000000000000000000000000000000000000000025f3e829f322ec86fa2ffd5a8000000000000000000000000000000000000000000000000000000000e7605df"
)

func TestTraceMatchesChainWithTheFullValidatorSet(t *testing.T) {
	trace := traceFullSetTx(t, func(*MockClient) {})

	require.Equal(t, "0xba7d", trace["gasUsed"])
	require.Equal(t, []string{fullSetOnChainLog}, callLogData(trace))
	require.Equal(t, []string{"STATICCALL 0x0000000000000000000000000000000000001007"}, callTargets(trace))
}

func TestTraceDependsOnTheReplayedValidatorSet(t *testing.T) {
	trace := traceFullSetTx(t, func(mc *MockClient) {
		truncateRecordedValidators(t, mc, int64(fullSetBlock)-1, 30)
	})

	require.NotEqual(t, []string{fullSetOnChainLog}, callLogData(trace))
}

// traceFullSetTx traces the transaction recorded in the block 242615775 fixture with
// callTracer, after adjust has edited the mock client, and returns the top-level frame.
func traceFullSetTx(t *testing.T, adjust func(*MockClient)) map[string]interface{} {
	s := SetupMockPacificTestServer(t, func(a *app.App, mc *MockClient) sdk.Context {
		ctx := a.RPCContextProvider(evmrpc.LatestCtxHeight).WithClosestUpgradeName(fullSetVersion)
		blockHeight := mockStatesFromBlockJson(ctx, fullSetBlock, a, mc)
		ctx = withCapturedConsensusParams(ctx, mc, blockHeight)
		removeFutureActivations(ctx, a, fullSetVersion)
		adjust(mc)
		return ctx.WithBlockHeight(blockHeight)
	})
	var trace map[string]interface{}
	s.Run(func(port int) {
		raw := sendRequestWithNamespace(
			"debug", port, "traceTransaction", fullSetTxHash,
			map[string]interface{}{
				"tracer":       "callTracer",
				"tracerConfig": map[string]interface{}{"withLog": true},
			},
		)
		res, ok := raw["result"].(map[string]interface{})
		require.True(t, ok, "trace failed: %v", raw)
		trace = res
	})
	return trace
}

// truncateRecordedValidators replaces the validator set recorded at height with its first n
// validators, presented as the whole set.
func truncateRecordedValidators(t *testing.T, mc *MockClient, height int64, n int) {
	key := validatorPageKey{height: height, page: 1, perPage: 100}
	recorded, ok := mc.mockedValidators[key]
	require.True(t, ok, "no validator set recorded at height %d", height)
	require.Greater(t, recorded.Total, n)
	mc.mockedValidators[key] = &coretypes.ResultValidators{
		BlockHeight: recorded.BlockHeight,
		Validators:  recorded.Validators[:n],
		Count:       n,
		Total:       n,
	}
}

// callLogData returns, in order, the data of every log in a callTracer frame and its subcalls.
func callLogData(frame map[string]interface{}) []string {
	var data []string
	if logs, ok := frame["logs"].([]interface{}); ok {
		for _, l := range logs {
			data = append(data, l.(map[string]interface{})["data"].(string))
		}
	}
	for _, sub := range subcalls(frame) {
		data = append(data, callLogData(sub)...)
	}
	return data
}

// callTargets returns the type and target of every call beneath a callTracer frame, in
// depth-first order.
func callTargets(frame map[string]interface{}) []string {
	var targets []string
	for _, sub := range subcalls(frame) {
		targets = append(targets, sub["type"].(string)+" "+sub["to"].(string))
		targets = append(targets, callTargets(sub)...)
	}
	return targets
}

// subcalls returns the calls a callTracer frame made.
func subcalls(frame map[string]interface{}) []map[string]interface{} {
	calls, _ := frame["calls"].([]interface{})
	subs := make([]map[string]interface{}, 0, len(calls))
	for _, c := range calls {
		subs = append(subs, c.(map[string]interface{}))
	}
	return subs
}

package statesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light"
	lightprovider "github.com/sei-protocol/sei-chain/sei-tendermint/light/provider"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// fakeLightProvider serves the light blocks of a fixed chain.
type fakeLightProvider struct {
	chain map[int64]*types.LightBlock
}

func (p fakeLightProvider) LightBlock(_ context.Context, height int64) (*types.LightBlock, error) {
	lb, ok := p.chain[height]
	if !ok {
		return nil, lightprovider.ErrLightBlockNotFound
	}
	return lb, nil
}

func (fakeLightProvider) ReportEvidence(context.Context, types.Evidence) error { return nil }

func (fakeLightProvider) ID() string { return "fake" }

var errParamsRequested = errors.New("consensus params were requested")

func TestChainTrustOptionsTakesThePeriodTheTrustedHeaderCommitsTo(t *testing.T) {
	params := *types.DefaultConsensusParams()
	params.StateSync.TrustPeriod = 42 * time.Hour
	chain := buildLightBlockChainWithParams(t.Context(), t, 1, 3, time.Now(), params)
	root := TrustRoot{Height: 1, Hash: chain[1].Hash()}

	type request struct {
		height int64
		hash   []byte
	}
	var requests []request
	source := func(_ context.Context, height int64, hash []byte) (types.ConsensusParams, error) {
		requests = append(requests, request{height: height, hash: hash})
		return params, nil
	}

	got, err := chainTrustOptions(t.Context(), root, fakeLightProvider{chain: chain}, source)
	require.NoError(t, err)
	require.Equal(t, light.TrustOptions{Period: 42 * time.Hour, Height: 1, Hash: root.Hash}, got)
	require.Equal(t, []request{{height: 1, hash: chain[1].ConsensusHash}}, requests)
}

func TestChainTrustOptionsRejectsABlockWithoutTheTrustHash(t *testing.T) {
	chain := buildLightBlockChain(t.Context(), t, 1, 3, time.Now())
	root := TrustRoot{Height: 1, Hash: chain[2].Hash()}
	source := func(context.Context, int64, []byte) (types.ConsensusParams, error) {
		return types.ConsensusParams{}, errParamsRequested
	}

	_, err := chainTrustOptions(t.Context(), root, fakeLightProvider{chain: chain}, source)
	require.ErrorIs(t, err, errTrustHashMismatch)
}

func TestChainTrustOptionsFailsWhenTheChainSetsNoTrustPeriod(t *testing.T) {
	params := *types.DefaultConsensusParams()
	params.StateSync = types.StateSyncParams{}
	chain := buildLightBlockChainWithParams(t.Context(), t, 1, 3, time.Now(), params)
	root := TrustRoot{Height: 1, Hash: chain[1].Hash()}
	source := func(context.Context, int64, []byte) (types.ConsensusParams, error) {
		return params, nil
	}

	_, err := chainTrustOptions(t.Context(), root, fakeLightProvider{chain: chain}, source)
	require.ErrorIs(t, err, errNoTrustPeriod)
}

// fakeParamsClient serves fixed consensus params, or fails with err when it is set.
type fakeParamsClient struct {
	remote string
	params types.ConsensusParams
	err    error
}

func (c fakeParamsClient) ConsensusParams(_ context.Context, height *int64) (*coretypes.ResultConsensusParams, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &coretypes.ResultConsensusParams{BlockHeight: *height, ConsensusParams: c.params}, nil
}

func (c fakeParamsClient) Remote() string { return c.remote }

func TestRPCParamsReturnsTheFirstParamsMatchingTheHash(t *testing.T) {
	want := *types.DefaultConsensusParams()
	forged := want
	forged.StateSync.TrustPeriod = 1000 * time.Hour
	source := rpcParams([]consensusParamsClient{
		fakeParamsClient{remote: "unreachable", err: errors.New("connection refused")},
		fakeParamsClient{remote: "forging", params: forged},
		fakeParamsClient{remote: "honest", params: want},
	})

	got, err := source(t.Context(), 1, want.HashConsensusParams())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestRPCParamsFailsWhenNoServerMatchesTheHash(t *testing.T) {
	want := *types.DefaultConsensusParams()
	forged := want
	forged.StateSync.TrustPeriod = 1000 * time.Hour
	source := rpcParams([]consensusParamsClient{
		fakeParamsClient{remote: "forging", params: forged},
	})

	_, err := source(t.Context(), 1, want.HashConsensusParams())
	require.ErrorIs(t, err, errParamsHashMismatch)
}

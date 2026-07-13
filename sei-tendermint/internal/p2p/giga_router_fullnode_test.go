package p2p

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// TestGigaRouter_Fullnode covers the construction shape of the non-validator
// (fullnode) GigaRouter: routing always picks a remote shard owner (no
// local short-circuit because there is no validator key), data + service
// are constructed but consensus/producer are not, and the read-path
// passthrough methods source values from the local data.State + genesis
// doc (no errFullnode-style sentinels). The end-to-end block-sync /
// executeBlock behaviour is covered by the autobahn integration test
// where a real validator cluster supplies finalized blocks; this unit
// test only verifies the construction surface.
func TestGigaRouter_Fullnode(t *testing.T) {
	rng := utils.TestRng()
	_, validatorKeys := atypes.GenCommittee(rng, 5)
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	urlByValidator := map[atypes.PublicKey]*url.URL{}
	for i, validatorKey := range validatorKeys {
		nodeKey := makeKey(rng)
		// Every committee member needs an EVMRPC URL for fullnode mode —
		// NewGigaRouter enforces this at construction so a missing URL
		// can't lead to silently-dropped txs.
		rpcURL, err := url.Parse(fmt.Sprintf("http://validator-%d.example.com:8545", i))
		require.NoError(t, err)
		addrs[validatorKey.Public()] = GigaNodeAddr{
			Key:      nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname: "127.0.0.1", Port: 26657},
			EVMRPC:   rpcURL,
		}
		urlByValidator[validatorKey.Public()] = rpcURL
	}
	cp := types.DefaultConsensusParams()
	cp.Block.MaxGas = 12345
	genDoc := &types.GenesisDoc{
		ChainID:         "giga-router-fullnode-test",
		InitialHeight:   1,
		AppState:        testAppStateJSON(rng),
		ConsensusParams: cp,
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	app := newTestApp()
	proxyApp := proxy.New(app)

	// Fullnodes have no validator key and no Producer config.
	// App is required for executeBlock but isn't exercised by this test.
	router, err := NewGigaFullnodeRouter(&GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     addrs,
		PersistentStateDir: utils.Some(t.TempDir()),
		App:                proxyApp,
		GenDoc:             genDoc,
	}, makeKey(rng))
	require.NoError(t, err)

	// EvmProxy: for every sender, the fullnode router resolves to the
	// shard owner's URL. NewGigaRouter rejects configs where any
	// committee member is missing an EVMRPC URL, so the (nil,false)
	// branch is unreachable here. Crucially, no sender is ever proxied
	// "to ourselves" — that short-circuit doesn't exist in fullnode mode.
	expectedRemoteURLs := map[string]struct{}{}
	for _, rpcURL := range urlByValidator {
		expectedRemoteURLs[rpcURL.String()] = struct{}{}
	}
	returnedRemoteURLs := map[string]struct{}{}
	for range 200 {
		sender := common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		shardValidator := router.data.Registry().LatestEpoch().Committee().EvmShard(sender)
		expectedURL := urlByValidator[shardValidator]
		proxyURL, ok := router.EvmProxy(sender).Get()
		require.True(t, ok)
		require.Equal(t, expectedURL.String(), proxyURL.String())
		returnedRemoteURLs[proxyURL.String()] = struct{}{}
	}
	// Sanity: with 200 random senders mapped uniformly over 5 shards we
	// expect to have hit every shard owner at least once.
	require.Equal(t, expectedRemoteURLs, returnedRemoteURLs)

	// Read-path methods source from local data.State + genesis doc — no
	// sentinels. Before any block is pushed (and InitChain hasn't run),
	// app.LastBlockHeight() is 0, so LastCommittedBlockNumber returns 0.
	// MaxGasEstimatedPerBlock reflects the genesis consensus param.
	require.Equal(t, int64(0), router.LastCommittedBlockNumber())
	require.Equal(t, uint64(12345), router.MaxGasEstimatedPerBlock())
	// BlockByHash returns &ResultBlock{Block:nil} for an unknown hash, the
	// same shape the validator path returns — no sentinel mode-check.
	rb, err := router.BlockByHash(t.Context(), atypes.BlockHeaderHash{})
	require.NoError(t, err)
	require.Nil(t, rb.Block)
}

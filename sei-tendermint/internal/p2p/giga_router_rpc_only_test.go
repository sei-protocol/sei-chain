package p2p

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// TestGigaRouter_RPCOnly covers the construction shape of the non-validator
// (rpc-only) GigaRouter: routing always picks a remote shard owner (no
// local short-circuit because there is no validator key), data + service
// are constructed but consensus/producer are not, and the read-path
// passthrough methods source values from the local data.State + genesis
// doc (no errRPCOnly-style sentinels). The end-to-end block-sync /
// executeBlock behaviour is covered by the autobahn integration test
// where a real validator cluster supplies finalized blocks; this unit
// test only verifies the construction surface.
func TestGigaRouter_RPCOnly(t *testing.T) {
	rng := utils.TestRng()
	_, validatorKeys := atypes.GenCommittee(rng, 5)
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	urlByValidator := map[atypes.PublicKey]*url.URL{}
	for i, validatorKey := range validatorKeys {
		nodeKey := makeKey(rng)
		// Every committee member needs an EVMRPC URL for rpc-only mode —
		// NewGigaRouter enforces this at construction so a missing URL
		// can't lead to silently-dropped txs.
		rpcURL, err := url.Parse(fmt.Sprintf("http://validator-%d.example.com:8545", i))
		require.NoError(t, err)
		addrs[validatorKey.Public()] = GigaNodeAddr{
			Key:      nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname: "127.0.0.1", Port: 26657},
			EVMRPC:   utils.Some(rpcURL),
		}
		urlByValidator[validatorKey.Public()] = rpcURL
	}
	cp := types.DefaultConsensusParams()
	cp.Block.MaxGas = 12345
	genDoc := &types.GenesisDoc{
		ChainID:         "giga-router-rpc-only-test",
		InitialHeight:   1,
		AppState:        testAppStateJSON(rng),
		ConsensusParams: cp,
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	app := newTestApp()
	txMempool := mempool.NewTxMempool(mempool.TestConfig(), proxy.New(app, proxy.NopMetrics()), mempool.NopMetrics(), mempool.NopTxConstraintsFetcher)

	// Construct with no validator key (rpc-only nodes don't have one) and no
	// Producer config. Consensus is set to an empty struct so the data WAL
	// reads PersistentStateDir = None (in-memory) — fine for the
	// construction-shape check.
	routerIface, err := NewGigaRouter(&GigaRouterConfig{
		DialInterval:   time.Second,
		ValidatorAddrs: addrs,
		Consensus:      &consensus.Config{},
		TxMempool:      txMempool,
		GenDoc:         genDoc,
		RPCOnly:        true,
	}, makeKey(rng))
	require.NoError(t, err)

	// Shape: rpc-only routers run data + a block-sync service (outbound only)
	// to pull finalized blocks from committee members. NewGigaRouter returns
	// the rpc-only concrete impl through the interface; assert on the
	// concrete type so we can inspect the shared common fields.
	require.True(t, routerIface.IsRPCOnly())
	router, ok := routerIface.(*gigaRPCOnlyRouter)
	require.True(t, ok, "rpc-only NewGigaRouter should return *gigaRPCOnlyRouter")
	require.NotNil(t, router.data)
	require.NotNil(t, router.service)
	require.NotNil(t, router.poolOut)

	// EvmProxy: for every sender, the rpc-only router resolves to the
	// shard owner's URL. NewGigaRouter rejects configs where any
	// committee member is missing an EVMRPC URL, so the (nil,false)
	// branch is unreachable here. Crucially, no sender is ever proxied
	// "to ourselves" — that short-circuit doesn't exist in rpc-only mode.
	expectedRemoteURLs := map[string]struct{}{}
	for _, rpcURL := range urlByValidator {
		expectedRemoteURLs[rpcURL.String()] = struct{}{}
	}
	returnedRemoteURLs := map[string]struct{}{}
	for range 200 {
		sender := common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		shardValidator := router.data.Committee().EvmShard(sender)
		expectedURL := urlByValidator[shardValidator]
		proxyURL, ok := router.EvmProxy(sender)
		require.True(t, ok)
		require.Equal(t, expectedURL.String(), proxyURL.String())
		returnedRemoteURLs[proxyURL.String()] = struct{}{}
	}
	// Sanity: with 200 random senders mapped uniformly over 5 shards we
	// expect to have hit every shard owner at least once.
	require.Equal(t, expectedRemoteURLs, returnedRemoteURLs)

	// Read-path methods source from local data.State + genesis doc — no
	// sentinels. Before any block is pushed, LastCommittedBlockNumber is
	// InitialHeight - 1 (0 for genesis at 1); MaxGasPerBlock is the genesis
	// consensus param.
	require.Equal(t, int64(0), router.LastCommittedBlockNumber())
	require.Equal(t, int64(12345), router.MaxGasPerBlock())
	// BlockByHash returns &ResultBlock{Block:nil} for an unknown hash, the
	// same shape the validator path returns — no sentinel mode-check.
	rb, err := router.BlockByHash(t.Context(), atypes.BlockHeaderHash{})
	require.NoError(t, err)
	require.Nil(t, rb.Block)
}

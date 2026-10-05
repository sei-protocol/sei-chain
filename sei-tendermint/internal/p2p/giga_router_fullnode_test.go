package p2p

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
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
	urlByValidator := map[atypes.PublicKey]url.URL{}
	for i, validatorKey := range validatorKeys {
		nodeKey := makeKey(rng)
		// Every committee member needs an EVMRPC URL for fullnode mode —
		// NewGigaRouter enforces this at construction so a missing URL
		// can't lead to silently-dropped txs.
		rpcURL := *utils.OrPanic1(url.Parse(fmt.Sprintf("http://validator-%d.example.com:8545", i)))
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

	cfg, _, blockStore := newTestGigaConfig(t, addrs, genDoc)
	dataState, err := BuildDataState(cfg, blockStore)
	require.NoError(t, err)

	// Fullnodes have no validator key and no Producer config.
	// App is required for executeBlock but isn't exercised by this test.
	router, err := NewGigaFullnodeRouter(cfg, makeKey(rng), dataState)
	require.NoError(t, err)
	clientByValidator := map[atypes.PublicKey]*ethrpc.Client{}
	for validator, rpcURL := range urlByValidator {
		clientByValidator[validator] = registerEvmProxyForTest(t, router.gigaRouterCommon, validator, rpcURL)
	}

	// EvmProxy: for every sender, the fullnode router resolves to the
	// shard owner's client. NewGigaRouter rejects configs where any
	// committee member is missing an EVMRPC URL, so the (nil,false)
	// branch is unreachable here. Crucially, no sender is ever proxied
	// "to ourselves" — that short-circuit doesn't exist in fullnode mode.
	expectedRemoteClients := map[*ethrpc.Client]struct{}{}
	for _, client := range clientByValidator {
		expectedRemoteClients[client] = struct{}{}
	}
	returnedRemoteClients := map[*ethrpc.Client]struct{}{}
	for range 200 {
		sender := common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		shardValidator := router.data.NextCommitEpoch().Load().Committee().EvmShard(sender)
		expectedClient := clientByValidator[shardValidator]
		proxyClient, ok := router.EvmProxy(sender).Get()
		require.True(t, ok)
		require.Equal(t, expectedClient, proxyClient)
		returnedRemoteClients[proxyClient] = struct{}{}
	}
	// Sanity: with 200 random senders mapped uniformly over 5 shards we
	// expect to have hit every shard owner at least once.
	require.Equal(t, expectedRemoteClients, returnedRemoteClients)

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

// TestGigaRouter_FullnodeSyncsFromEveryCommitteeMember runs a fullnode beside a
// validator cluster. The fullnode holds a block-sync connection to every
// committee member at once, each validator serves it from one inbound fullnode
// slot, and the fullnode executes the same chain as the validators.
func TestGigaRouter_FullnodeSyncsFromEveryCommitteeMember(t *testing.T) {
	const maxTxsPerBlock = 20
	const blocksPerLane = 5
	const txGasUsed = 21_000

	ctx := t.Context()
	rng := utils.TestRng()
	_, keys := atypes.GenCommittee(rng, 4)
	var validators []*testNodeCfg
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	for _, key := range keys {
		v := &testNodeCfg{validatorKey: key, nodeKey: makeKey(rng), addr: tcp.TestReserveAddr()}
		validators = append(validators, v)
		addrs[key.Public()] = v.GigaNodeAddr()
	}
	fullnode := &testNodeCfg{nodeKey: makeKey(rng), addr: tcp.TestReserveAddr()}
	genDoc := &types.GenesisDoc{
		ChainID:       "giga-router-fullnode-sync-test",
		InitialHeight: rng.Int63n(100000) + 1,
		AppState:      testAppStateJSON(rng),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var validatorRouters []*gigaValidatorRouter
		var validatorApps []*testApp
		var allTxs [][]byte
		for i, v := range validators {
			cfg, app, blockStore := newTestGigaConfig(t, addrs, genDoc)
			// One slot: the fullnode is the only non-committee peer.
			cfg.MaxInboundFullnodePeers = 1
			dataState, err := BuildDataState(cfg, blockStore)
			require.NoError(t, err, "BuildDataState[%v]", i)
			giga, err := NewGigaValidatorRouter(&GigaValidatorConfig{
				GigaRouterCommonConfig: *cfg,
				ValidatorKey:           v.validatorKey,
				ViewTimeout:            func(atypes.View) time.Duration { return time.Hour },
				Producer:               testProducerConfig(txGasUsed, maxTxsPerBlock),
			}, v.nodeKey, dataState)
			require.NoError(t, err, "NewGigaValidatorRouter[%v]", i)
			spawnTestRouter(ctx, t, s, fmt.Sprint(i), v, genDoc.ChainID, giga)
			validatorRouters = append(validatorRouters, giga)
			validatorApps = append(validatorApps, app)
			var txs [][]byte
			for range maxTxsPerBlock * blocksPerLane {
				tx := utils.GenBytes(rng, 100)
				txs = append(txs, tx)
				allTxs = append(allTxs, tx)
			}
			s.SpawnNamed(fmt.Sprintf("producer[%v]", i), func() error {
				for _, tx := range txs {
					if _, err := giga.producer.InsertTx(ctx, tx); err != nil {
						return fmt.Errorf("producer.InsertTx(): %w", err)
					}
				}
				return nil
			})
		}
		cfg, fullnodeApp, blockStore := newTestGigaConfig(t, addrs, genDoc)
		dataState, err := BuildDataState(cfg, blockStore)
		require.NoError(t, err, "BuildDataState[fullnode]")
		fullnodeRouter, err := NewGigaFullnodeRouter(cfg, fullnode.nodeKey, dataState)
		require.NoError(t, err, "NewGigaFullnodeRouter")
		spawnTestRouter(ctx, t, s, "fullnode", fullnode, genDoc.ChainID, fullnodeRouter)

		// The fullnode executes every transaction and ends in the validators' state.
		for _, app := range append(validatorApps, fullnodeApp) {
			for _, tx := range allTxs {
				require.NoError(t, app.WaitForTx(ctx, tx), "WaitForTx")
			}
		}
		require.NoError(t, utils.TestDiff(validatorApps[0].Snapshot(), fullnodeApp.Snapshot()), "fullnode state mismatch")

		// The fullnode holds an outbound connection to every committee member
		// at once, and every validator serves it from its fullnode pool.
		connectedToAll := func() bool {
			for i, v := range validators {
				if _, ok := fullnodeRouter.poolOut.Get(v.validatorKey.Public()); !ok {
					return false
				}
				if _, ok := validatorRouters[i].poolIn.Get(fullnode.nodeKey.Public()); !ok {
					return false
				}
			}
			return true
		}
		if err := utils.WithTimeout(ctx, 30*time.Second, func(ctx context.Context) error {
			for !connectedToAll() {
				if err := utils.Sleep(ctx, 10*time.Millisecond); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("fullnode is not connected to every committee member: %w", err)
		}
		for i, giga := range validatorRouters {
			require.Equal(t, int64(1), giga.inboundFullnodeCount.Load(), "router[%v].inboundFullnodeCount", i)
		}
		return nil
	})
	require.NoError(t, err)
}

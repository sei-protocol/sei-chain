package p2p

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	gigapb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
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
		rpcURL := *utils.OrPanic1(url.Parse(fmt.Sprintf("http://validator-%d.example.com:8545", i)))
		addrs[validatorKey.Public()] = GigaNodeAddr{
			Key:      nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname: "127.0.0.1", Port: 26657},
		}
		urlByValidator[validatorKey.Public()] = rpcURL
	}
	cp := testGigaConsensusParams()
	cp.Block.MaxGas = 12345
	genDoc := &types.GenesisDoc{
		ChainID:         "giga-router-fullnode-test",
		InitialHeight:   1,
		AppState:        testAppStateJSON(rng),
		ConsensusParams: cp,
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	app := newTestApp(genDoc.AppState)
	proxyApp := abevm.NewProxy(app)

	dir := t.TempDir()
	// Same resolve path as config.AutobahnBlockDBConfig{}.LittBlockConfig
	// (zero overrides); p2p can't import config (import cycle).
	littCfg, err := blockstore.DefaultConfig(filepath.Join(dir, "blockdb"))
	require.NoError(t, err)
	littCfg.Litt.Fsync = true
	blockStore, err := blockstore.New(littCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blockStore.Close() })
	cfg := &GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     addrs,
		PersistentStateDir: dir,
		App:                proxyApp,
		AppHashBuilder:     newPublishingAppHashBuilder(),
		GenDoc:             genDoc,
		EnableEvmProxy:     true,
	}
	registry := testGenesisEpochs(t, genDoc, addrs)
	dataState, err := BuildDataState(cfg, blockStore, abevm.TestEpochApp(registry), genDoc.GenesisTime)
	require.NoError(t, err)

	// Fullnodes have no validator key and no Producer config.
	// App is required for execution but isn't exercised by this test.
	router, err := NewGigaFullnodeRouter(cfg, makeKey(rng), dataState, newTestSigner(cfg.App.EvmChainID(), 0))
	require.NoError(t, err)
	clientByValidator := map[atypes.PublicKey]*ethrpc.Client{}
	for validator, rpcURL := range urlByValidator {
		clientByValidator[validator] = registerEvmProxyForTest(t, router.GigaRouterCommon, validator, rpcURL)
	}

	// EvmProxy: for every sender, the fullnode router resolves to the
	// shard owner's connected client. No sender is ever proxied
	// "to ourselves" — that short-circuit doesn't exist in fullnode mode.
	expectedRemoteClients := map[*ethrpc.Client]struct{}{}
	for _, client := range clientByValidator {
		expectedRemoteClients[client] = struct{}{}
	}
	returnedRemoteClients := map[*ethrpc.Client]struct{}{}
	for range 200 {
		sender := common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		shardValidator := router.data.Anchor().Load().NextHeightEpoch().Committee().EvmShard(sender)
		expectedClient := clientByValidator[shardValidator]
		proxyClient, ok := router.evmProxy(sender).Get()
		require.True(t, ok)
		require.Equal(t, expectedClient, proxyClient)
		returnedRemoteClients[proxyClient] = struct{}{}
	}
	// Sanity: with 200 random senders mapped uniformly over 5 shards we
	// expect to have hit every shard owner at least once.
	require.Equal(t, expectedRemoteClients, returnedRemoteClients)

	// BlockByHash returns None for an unknown hash on both router roles.
	rb, err := router.BlockByHash(t.Context(), common.Hash{})
	require.NoError(t, err)
	require.False(t, rb.IsPresent())
}

func TestGigaFullnode_RPCConnectionsAndBlockSyncWithAllValidators(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := atypes.GenCommittee(rng, 2)
	state, _ := testDataState(t, committee)
	opened := make(chan string, 2)
	closed := make(chan string, 2)
	rpcServer := ethrpc.NewServer()
	t.Cleanup(rpcServer.Stop)
	wsHandler := rpcServer.WebsocketHandler([]string{"*"})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opened <- r.URL.Path
		defer func() { closed <- r.URL.Path }()
		wsHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	endpoint := *utils.OrPanic1(url.Parse("ws" + strings.TrimPrefix(httpServer.URL, "http")))
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	var listeners []*tcp.Listener
	var nodeKeys []NodeSecretKey
	var disconnect []chan struct{}
	for _, key := range keys {
		nodeKey := makeKey(rng)
		addr := tcp.TestReserveAddr()
		listener, err := tcp.Listen(addr)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, listener.Close()) })
		listeners = append(listeners, listener)
		nodeKeys = append(nodeKeys, nodeKey)
		disconnect = append(disconnect, make(chan struct{}))
		addrs[key.Public()] = GigaNodeAddr{Key: nodeKey.Public(), HostPort: tcp.HostPort{Hostname: addr.Addr().String(), Port: addr.Port()}}
	}
	router, err := NewGigaFullnodeRouter(&GigaRouterCommonConfig{
		ValidatorAddrs:  addrs,
		AllowLocalAddrs: true,
		DialInterval:    time.Second,
		App:             abevm.NewProxy(newTestApp(testAppStateJSON(rng))),
	}, makeKey(rng), state, newTestSigner(1, 0))
	require.NoError(t, err)
	subscribed := make(chan int, 2)
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		for i, listener := range listeners {
			s.SpawnBg(func() error {
				c, err := listener.AcceptOrClose(ctx)
				if err != nil {
					return err
				}
<<<<<<< HEAD
				return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, peerScope scope.Scope) error {
					peerScope.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, c.Run(ctx)) })
=======
				return utils.IgnoreAfterCancel(ctx, utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, peerScope scope.Scope) error {
					peerScope.SpawnBg(func() error { return c.Run(ctx) })
>>>>>>> origin/main
					peerURL := endpoint
					peerURL.Path = fmt.Sprintf("/%d", i)
					hConn, err := handshake(ctx, c, handshakeSpec{
						NodeKey: nodeKeys[i],
						Giga: utils.Some(gigaHandshakeSpec{
							ValidatorKey: utils.Some(keys[i]),
							EVMRPC:       peerURL,
						}),
						IsServer: true,
					})
					if err != nil {
						return err
					}
					server := rpc.NewServer[giga.API]()
					peerScope.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, server.Run(ctx, hConn.conn)) })
					peerScope.SpawnBg(func() error {
						return giga.StreamFullCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.FullCommitQC, *gigapb.StreamFullCommitQCsReq]) error {
							if _, err := stream.Recv(ctx); err != nil {
								return err
							}
							if err := utils.Send(ctx, subscribed, i); err != nil {
								return err
							}
							<-ctx.Done()
							return ctx.Err()
						})
					})
					_, _, err = utils.RecvOrClosed(ctx, disconnect[i])
					return err
				})))
			})
		}
		s.SpawnBg(func() error { return utils.IgnoreCancel(router.runCommitteeConnections(ctx)) })
		for range keys {
			if _, err := utils.Recv(ctx, opened); err != nil {
				return err
			}
		}
		first, err := utils.Recv(ctx, subscribed)
		if err != nil {
			return err
		}
		next, err := utils.Recv(ctx, subscribed)
		if err != nil {
			return err
		}
		if next == first {
			return fmt.Errorf("block-sync must run with both validators")
		}
		close(disconnect[first])
		path, err := utils.Recv(ctx, closed)
		if err != nil {
			return err
		}
		if path != fmt.Sprintf("/%d", first) {
			return fmt.Errorf("closed RPC endpoint %s, want /%d", path, first)
		}
		return nil
	}))
	_, err = utils.Recv(t.Context(), closed)
	require.NoError(t, err)
	for _, key := range keys {
		for proxies := range router.proxies.RLock() {
			_, present := proxies[key.Public()]
			require.False(t, present)
		}
	}
}

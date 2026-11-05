package p2p

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
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

	app := newTestApp()
	proxyApp := proxy.New(app)

	dir := t.TempDir()
	// Same resolve path as config.AutobahnBlockDBConfig{}.LittBlockConfig
	// (zero overrides); p2p can't import config (import cycle).
	littCfg, err := littblock.DefaultConfig(filepath.Join(dir, "blockdb"))
	require.NoError(t, err)
	littCfg.Litt.Fsync = true
	db, err := littblock.NewBlockDB(littCfg)
	require.NoError(t, err)
	blockStore, err := blockstore.New(db)
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
		shardValidator := router.data.NextCommitEpoch().Committee().EvmShard(sender)
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
	rb, err := router.BlockByHash(t.Context(), common.Hash{})
	require.NoError(t, err)
	require.Nil(t, rb.Block)
}

// TestGigaFullnodeRouter_CurrentCommitteeOnly runs the fullnode. A validator who
// leaves the commit committee but remains in the anchor committee loses the
// block-sync connection and the EVM proxy.
func TestGigaFullnodeRouter_CurrentCommitteeOnly(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	b := atypes.GenSecretKey(rng).Public()
	aGiga, aGigaHP := reserveTCP(t)
	bGiga, bGigaHP := reserveTCP(t)
	aEVM, aEVMURL, closeAEVM := reserveWebsocket(t)
	bEVM, bEVMURL, closeBEVM := reserveWebsocket(t)

	commit := utils.NewAtomicSend(testEpoch(2, map[atypes.PublicKey]uint64{a: 1}))
	anchor := utils.NewAtomicSend(testAnchor(testEpoch(2, map[atypes.PublicKey]uint64{a: 1})))
	committee, err := atypes.NewCommittee(map[atypes.PublicKey]uint64{atypes.GenSecretKey(rng).Public(): 1})
	require.NoError(t, err)
	reg, err := epoch.NewRegistry(committee, 1, time.Now(), utils.None[string]())
	require.NoError(t, err)
	db, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	state, err := data.NewState(&data.Config{Registry: reg}, db)
	require.NoError(t, err)
	genDoc := &types.GenesisDoc{
		ChainID:         "fullnode-current-committee",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: types.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	router, err := NewGigaFullnodeRouter(&GigaRouterCommonConfig{
		DialInterval: time.Millisecond,
		ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public(), HostPort: aGigaHP, EVMRPC: aEVMURL},
			b: {Key: makeKey(rng).Public(), HostPort: bGigaHP, EVMRPC: bEVMURL},
		},
		GenDoc:         genDoc,
		App:            proxy.New(&fixedHeightApp{}),
		AppHashBuilder: newPublishingAppHashBuilder(),
	}, makeKey(rng), state)
	require.NoError(t, err)
	router.nextCommitEpoch = commit.Subscribe()
	router.registry = idleRegistry(t)
	router.anchor = anchor.Subscribe()

	aGigaAccepted := make(chan struct{})
	bGigaAccepted := make(chan struct{})
	aEVMAccepted := make(chan struct{})
	aEVMClosed := make(chan struct{})
	err = scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		runErr := make(chan error, 1)
		s.SpawnBg(func() error {
			err := router.Run(ctx)
			if err != nil && ctx.Err() == nil {
				runErr <- err
			}
			return nil
		})
		s.SpawnBg(func() error { return utils.IgnoreCancel(acceptGiga(ctx, aGiga, aGigaAccepted)) })
		s.SpawnBg(func() error { return utils.IgnoreCancel(acceptGiga(ctx, bGiga, bGigaAccepted)) })
		s.SpawnBg(func() error { return holdWebsocket(ctx, aEVM, closeAEVM, aEVMAccepted, aEVMClosed) })
		s.SpawnBg(func() error { return holdWebsocket(ctx, bEVM, closeBEVM, make(chan struct{}), make(chan struct{})) })
		wait := func(ch <-chan struct{}) error {
			select {
			case <-ch:
				return nil
			case err := <-runErr:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := wait(aGigaAccepted); err != nil {
			return err
		}
		if err := wait(aEVMAccepted); err != nil {
			return err
		}
		commit.Store(testEpoch(3, map[atypes.PublicKey]uint64{b: 1}))
		anchor.Store(testAnchor(testEpoch(2, map[atypes.PublicKey]uint64{a: 1})))
		if err := wait(aEVMClosed); err != nil {
			return err
		}
		return wait(bGigaAccepted)
	})
	require.NoError(t, err)
}

func reserveTCP(t *testing.T) (*tcp.Listener, tcp.HostPort) {
	t.Helper()
	addr := tcp.TestReserveAddr()
	ln, err := tcp.Listen(addr)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ln.Close()) })
	return ln, tcp.HostPort{Hostname: addr.Addr().String(), Port: addr.Port()}
}

func reserveWebsocket(t *testing.T) (net.Listener, url.URL, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var once sync.Once
	closeLn := func() { once.Do(func() { _ = ln.Close() }) }
	t.Cleanup(closeLn)
	return ln, *utils.OrPanic1(url.Parse("ws://" + ln.Addr().String())), closeLn
}

func acceptGiga(ctx context.Context, ln *tcp.Listener, accepted chan struct{}) error {
	conn, err := ln.AcceptOrClose(ctx)
	if err != nil {
		return err
	}
	close(accepted)
	return conn.Run(ctx)
}

func holdWebsocket(ctx context.Context, ln net.Listener, closeLn func(), accepted, closed chan struct{}) error {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var acceptedOnce sync.Once
	var closedOnce sync.Once
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		acceptedOnce.Do(func() { close(accepted) })
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				closedOnce.Do(func() { close(closed) })
				return
			}
		}
	})}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			<-ctx.Done()
			closeLn()
			return nil
		})
		err := server.Serve(ln)
		if ctx.Err() != nil {
			return nil
		}
		return err
	})
}

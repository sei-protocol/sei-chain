package p2p

import (
	"context"
	"fmt"
	"maps"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"
)

func TestGigaRouter_FinalizeBlocks(t *testing.T) {
	const maxTxsPerBlock = 20
	const blocksPerLane = 5
	const txGasUsed = 21_000

	ctx := t.Context()
	rng := utils.TestRng()
	_, keys := atypes.GenCommittee(rng, 4)
	var cfgs []*testNodeCfg
	for _, key := range keys {
		cfgs = append(cfgs, &testNodeCfg{
			validatorKey: key,
			nodeKey:      makeKey(rng),
			addr:         tcp.TestReserveAddr(),
		})
	}
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	for _, cfg := range cfgs {
		addrs[cfg.validatorKey.Public()] = cfg.GigaNodeAddr()
	}
	genDoc := &types.GenesisDoc{
		ChainID:       "giga-router-test",
		InitialHeight: rng.Int63n(100000) + 1,
		AppState:      testAppStateJSON(rng),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var apps []*testApp
		var gigas []*gigaValidatorRouter
		var executors []*execution.Executor
		var allTxs [][]byte
		for i, cfg := range cfgs {
			nodeInfo := makeInfo(cfg.nodeKey)
			nodeInfo.ListenAddr = cfg.addr.String()
			nodeInfo.Network = genDoc.ChainID
			e := Endpoint{AddrPort: cfg.addr}
			app := newTestApp()
			proxyApp := proxy.New(app)
			// In giga mode the CometBFT handshaker is skipped; the executor
			// calls InitChain itself on fresh start.
			dir := t.TempDir()
			// Same resolve path as config.AutobahnBlockDBConfig{}.LittBlockConfig
			// (zero overrides); p2p can't import config (import cycle).
			littCfg, err := blockstore.DefaultConfig(filepath.Join(dir, "blockdb"))
			if err != nil {
				return fmt.Errorf("blockstore.DefaultConfig[%v]: %w", i, err)
			}
			littCfg.Litt.Fsync = true
			blockStore, err := blockstore.New(littCfg)
			if err != nil {
				return fmt.Errorf("blockstore.New[%v]: %w", i, err)
			}
			t.Cleanup(func() { _ = blockStore.Close() })
			commonCfg := GigaRouterCommonConfig{
				// Aggressive dialing rate to speed up startup.
				DialInterval:       100 * time.Millisecond,
				ValidatorAddrs:     addrs,
				PersistentStateDir: dir,
				App:                proxyApp,
				AppHashBuilder:     newPublishingAppHashBuilder(),
				GenDoc:             genDoc,
				EnableEvmProxy:     true,
			}
			executor := newTestExecutor(&commonCfg, blockStore)
			dataState, err := BuildDataState(&commonCfg, blockStore)
			if err != nil {
				return fmt.Errorf("BuildDataState[%v]: %w", i, err)
			}
			giga, err := NewGigaValidatorRouter(&GigaValidatorConfig{
				GigaRouterCommonConfig: commonCfg,
				ValidatorKey:           cfg.validatorKey,
				ViewTimeout:            func(atypes.View) time.Duration { return time.Hour },
				ProposalTimeout:        time.Hour,
				MaxRoadBlocksRate:      rate.Inf,
				Producer: &producer.Config{
					MaxGasWantedPerBlock:    txGasUsed * maxTxsPerBlock,
					MaxGasEstimatedPerBlock: txGasUsed * maxTxsPerBlock,
					MaxTxsPerBlock:          maxTxsPerBlock,
					MaxTxsPerSecond:         utils.None[uint64](),
					BlockInterval:           100 * time.Millisecond,
					AllowEmptyBlocks:        false,
				},
			}, cfg.nodeKey, dataState)
			if err != nil {
				return fmt.Errorf("NewGigaValidatorRouter[%v]: %w", i, err)
			}
			router, err := NewRouter(
				cfg.nodeKey,
				func() *types.NodeInfo { return &nodeInfo },
				dbm.NewMemDB(),
				&RouterOptions{
					SelfAddress:              utils.Some(e.NodeAddress(cfg.nodeKey.Public().NodeID())),
					Endpoint:                 e,
					Connection:               conn.DefaultMConnConfig(),
					IncomingConnectionWindow: utils.Some(time.Duration(0)),
					MaxAcceptRate:            rate.Inf,
					MaxDialRate:              rate.Limit(30),
					Giga:                     utils.Some[GigaRouter](giga),
				},
			)
			if err != nil {
				return fmt.Errorf("NewRouter[%v]: %w", i, err)
			}
			s.SpawnBgNamed(fmt.Sprintf("router[%v]", i), func() error { return utils.IgnoreCancel(router.Run(ctx)) })
			s.SpawnBgNamed(fmt.Sprintf("giga[%v]", i), func() error { return utils.IgnoreCancel(giga.Run(ctx)) })
			s.SpawnBgNamed(fmt.Sprintf("execution[%v]", i), func() error { return utils.IgnoreCancel(executor.Run(ctx)) })
			apps = append(apps, app)
			gigas = append(gigas, giga)
			executors = append(executors, executor)
			var txs [][]byte
			for range maxTxsPerBlock * blocksPerLane {
				tx := utils.GenBytes(rng, 100)
				txs = append(txs, tx)
				allTxs = append(allTxs, tx)
			}
			s.SpawnNamed(fmt.Sprintf("producer[%v]", i), func() error {
				v := giga.Mempool().OrPanic("validator giga must have a mempool")
				if err := v.WaitForMempool(ctx); err != nil {
					return fmt.Errorf("producer.WaitForMempool(): %w", err)
				}
				for _, tx := range txs {
					if _, err := v.InsertTx(ctx, tx); err != nil {
						return fmt.Errorf("producer.InsertTx(): %w", err)
					}
				}
				return nil
			})
		}
		// Each node should finalize all txs locally.
		for _, app := range apps {
			for _, tx := range allTxs {
				if err := app.WaitForTx(ctx, tx); err != nil {
					return fmt.Errorf("WaitForTx: %w", err)
				}
			}
		}
		// Nodes should agree on the final state.
		want := apps[0].Snapshot()
		for i, app := range apps {
			t.Logf("app[%v]", i)
			if err := utils.TestDiff(want, app.Snapshot()); err != nil {
				return fmt.Errorf("state mismatch app[%v]: %w", i, err)
			}
		}
		// Covers GigaRouter.LastCommittedBlockNumber() — after blocks have
		// been finalized every node should report a non-zero
		// consensus-committed height through the accessor used by /status.
		for i, giga := range gigas {
			committed := giga.LastCommittedBlockNumber()
			if committed <= 0 {
				return fmt.Errorf("router[%v].LastCommittedBlockNumber(): got %v, want positive", i, committed)
			}
			// Covers Executor.ExecutedBlocks — the watch behind
			// eth_subscribe("newHeads"). It must reach the committed height,
			// and the block it names must be readable via BlockByNumber.
			executed, err := executors[i].ExecutedBlocks().Wait(ctx, func(w atypes.ExecutedBlocks) bool {
				return int64(w.Latest().Number) >= committed
			})
			if err != nil {
				return fmt.Errorf("executor[%v].ExecutedBlocks().Wait(): %w", i, err)
			}
			_, err = giga.BlockByNumber(ctx, executed.Latest().Number)
			if err != nil {
				return fmt.Errorf("router[%v].BlockByNumber(executed=%v): %w", i, executed.Latest().Number, err)
			}
			// Covers GigaRouter.BlockByNumber — the accessor used by the
			// Autobahn branch in env.Block to serve /block and evmrpc block
			// lookups. Fetch the last committed block and verify it carries
			// the expected height + hash, the right chain id, and that the
			// payload Txs round-tripped (we just submitted txs).
			rb, err := giga.BlockByNumber(ctx, atypes.RoadIndex(committed)) //nolint:gosec // committed is positive (validated above)
			if err != nil {
				return fmt.Errorf("router[%v].BlockByNumber(%v): %w", i, committed, err)
			}
			if rb.Block == nil {
				return fmt.Errorf("router[%v].BlockByNumber(%v).Block: unexpected nil", i, committed)
			}
			if err := utils.TestDiff(committed, rb.Block.Height); err != nil {
				return fmt.Errorf("router[%v].BlockByNumber(%v) height: %w", i, committed, err)
			}
			if len(rb.BlockID.Hash) == 0 {
				return fmt.Errorf("router[%v].BlockByNumber(%v) block hash: expected nonempty", i, committed)
			}
			if err := utils.TestDiff(genDoc.ChainID, rb.Block.Header.ChainID); err != nil {
				return fmt.Errorf("router[%v].BlockByNumber(%v) chain id: %w", i, committed, err)
			}
			// LastCommit is non-nil with empty Signatures — mirrors
			// executeBlock's FinalizeBlock(DecidedLastCommit: empty)
			// so trace replay and production both see "no votes" on
			// the prior block. ToReqBeginBlock skips the per-val loop
			// when Signatures is empty, so this is also enough to
			// avoid the OOB deref the original PR was guarding against.
			if rb.Block.LastCommit == nil {
				return fmt.Errorf("router[%v].BlockByNumber(%v) LastCommit: unexpected nil", i, committed)
			}
			if len(rb.Block.LastCommit.Signatures) != 0 {
				return fmt.Errorf("router[%v].BlockByNumber(%v) Signatures: expected empty", i, committed)
			}
			// Round-trip the just-fetched block hash back through
			// BlockByHash and assert we get the same ResultBlock back.
			var hashKey common.Hash
			copy(hashKey[:], rb.BlockID.Hash)
			rbh, err := giga.BlockByHash(ctx, hashKey)
			if err != nil {
				return fmt.Errorf("router[%v].BlockByHash(%x): %w", i, rb.BlockID.Hash, err)
			}
			if err := utils.TestDiff(rb.BlockID, rbh.BlockID); err != nil {
				return fmt.Errorf("router[%v].BlockByHash(%x) block ID: %w", i, rb.BlockID.Hash, err)
			}
			wantBlock, err := rb.Block.ToProto()
			if err != nil {
				return err
			}
			gotBlock, err := rbh.Block.ToProto()
			if err != nil {
				return err
			}
			if err := utils.TestDiff(wantBlock, gotBlock); err != nil {
				return fmt.Errorf("router[%v].BlockByHash(%x) ≠ BlockByNumber(%v): %w", i, rb.BlockID.Hash, committed, err)
			}
		}
		// Payload.Txs round-trips: for every retained block, the txs the
		// data layer holds (SuperBlock.Payload.Txs) must equal the txs
		// surfaced through BlockByNumber. Iterates the full retain window
		// rather than a fixed tail so the assertion holds regardless of
		// where producers placed the test txs. Reaches into giga0.data
		// directly — internal same-package access.
		giga0 := gigas[0]
		latest := giga0.LastCommittedBlockNumber()
		for h := int64(1); h <= latest; h++ {
			gbn := atypes.RoadIndex(h) //nolint:gosec // h is positive
			gb, err := giga0.data.SuperBlock(ctx, gbn)
			if err != nil {
				continue // pruned out of the retain window
			}
			rb, err := giga0.BlockByNumber(ctx, gbn)
			if err != nil {
				return fmt.Errorf("router[0].BlockByNumber(%v): %w", h, err)
			}
			// Convert rb.Block.Data.Txs ([]types.Tx) back to [][]byte
			// to compare against gb.Txs directly.
			rbBytes := make([][]byte, len(rb.Block.Data.Txs))
			for j, t := range rb.Block.Data.Txs {
				rbBytes[j] = t
			}
			if err := utils.TestDiff(gb.Txs, rbBytes); err != nil {
				return fmt.Errorf("router[0].BlockByNumber(%v).Block.Data.Txs ≠ data.SuperBlock(%v).Payload.Txs: %w", h, h, err)
			}
		}
		return nil
	})
	require.NoError(t, err)
}

func TestGigaRouter_EvmProxy(t *testing.T) {
	rng := utils.TestRng()
	_, validatorKeys := atypes.GenCommittee(rng, 10)
	var nodeKeys []NodeSecretKey
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	urlByValidator := map[atypes.PublicKey]url.URL{}
	// DialContext opens the websocket before the first call, so the URL has
	// to name a listener.
	rpcSrv := ethrpc.NewServer()
	t.Cleanup(rpcSrv.Stop)
	httpSrv := httptest.NewServer(rpcSrv.WebsocketHandler([]string{"*"}))
	t.Cleanup(httpSrv.Close)
	rpcURL := *utils.OrPanic1(url.Parse("ws" + strings.TrimPrefix(httpSrv.URL, "http")))
	// NewGigaRouter requires EVMRPC on every committee member in both
	// validator and fullnode modes.
	for _, validatorKey := range validatorKeys {
		nodeKey := makeKey(rng)
		nodeKeys = append(nodeKeys, nodeKey)
		addrs[validatorKey.Public()] = GigaNodeAddr{
			Key:      nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname: "127.0.0.1", Port: 26657},
			EVMRPC:   rpcURL,
		}
		urlByValidator[validatorKey.Public()] = rpcURL
	}
	genDoc := &types.GenesisDoc{
		ChainID:       "giga-router-proxy-test",
		InitialHeight: 1,
		AppState:      testAppStateJSON(rng),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	dir := t.TempDir()
	// Same resolve path as config.AutobahnBlockDBConfig{}.LittBlockConfig
	// (zero overrides); p2p can't import config (import cycle).
	littCfg, err := blockstore.DefaultConfig(filepath.Join(dir, "blockdb"))
	require.NoError(t, err)
	littCfg.Litt.Fsync = true
	blockStore, err := blockstore.New(littCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blockStore.Close() })
	app := newTestApp()
	commonCfg := GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     addrs,
		PersistentStateDir: dir,
		App:                proxy.New(app),
		AppHashBuilder:     newPublishingAppHashBuilder(),
		GenDoc:             genDoc,
		EnableEvmProxy:     true,
	}
	dataState, err := BuildDataState(&commonCfg, blockStore)
	require.NoError(t, err)

	router, err := NewGigaValidatorRouter(&GigaValidatorConfig{
		GigaRouterCommonConfig: commonCfg,
		ValidatorKey:           validatorKeys[0],
		ViewTimeout:            func(atypes.View) time.Duration { return time.Second },
		ProposalTimeout:        time.Hour,
		MaxRoadBlocksRate:      rate.Inf,
		Producer: &producer.Config{
			MaxGasWantedPerBlock:    1,
			MaxGasEstimatedPerBlock: 1,
			MaxTxsPerBlock:          1,
			MaxTxsPerSecond:         utils.None[uint64](),
			BlockInterval:           time.Second,
		},
	}, nodeKeys[0], dataState)
	require.NoError(t, err)

	localValidator := validatorKeys[0].Public()

	connectedRemote := map[atypes.PublicKey]*ethrpc.Client{}
	expectedRemoteClients := map[*ethrpc.Client]struct{}{}
	for validator, addr := range addrs {
		if validator == localValidator {
			continue
		}
		if len(connectedRemote) >= 3 {
			break
		}
		client := registerEvmProxyForTest(t, router.gigaRouterCommon, validator, addr.EVMRPC)
		connectedRemote[validator] = client
		expectedRemoteClients[client] = struct{}{}
	}

	err = scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		for validator := range connectedRemote {
			ready := make(chan struct{})
			s.SpawnBgNamed(fmt.Sprintf("poolOut[%s]", validator), func() error {
				var client rpc.Client[giga.API]
				return utils.IgnoreCancel(router.poolOut.InsertAndRun(ctx, validator, client, func(ctx context.Context) error {
					close(ready)
					<-ctx.Done()
					return ctx.Err()
				}))
			})
			if _, _, err := utils.RecvOrClosed(ctx, ready); err != nil {
				return err
			}
		}

		returnedRemoteClients := map[*ethrpc.Client]struct{}{}
		seenDisconnected := false
		for range 400 {
			sender := common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
			shardValidator := router.data.Anchor().Load().NextIndexEpoch().Committee().EvmShard(sender)

			proxyClient, ok := router.EvmProxy(sender).Get()

			if shardValidator == localValidator {
				// Self-shard: validator short-circuits to local mempool.
				require.False(t, ok, "expected local shard %s to avoid proxying", shardValidator)
				require.Nil(t, proxyClient)
			} else if expectedClient, connected := connectedRemote[shardValidator]; !connected {
				require.False(t, ok, "expected disconnected shard %s to avoid proxying", shardValidator)
				require.Nil(t, proxyClient)
				seenDisconnected = true
			} else {
				require.True(t, ok, "expected connected shard %s to proxy", shardValidator)
				require.NotNil(t, proxyClient)
				require.Equal(t, expectedClient, proxyClient)
				returnedRemoteClients[proxyClient] = struct{}{}
			}
		}

		require.True(t, maps.Equal(expectedRemoteClients, returnedRemoteClients), "returned remote clients = %v, want %v", returnedRemoteClients, expectedRemoteClients)
		require.True(t, seenDisconnected, "expected at least one disconnected shard sample")
		return nil
	})
	require.NoError(t, err)
}

func TestGigaRouter_RejectsInvalidSelfAddr(t *testing.T) {
	rng := utils.TestRng()
	_, validatorKeys := atypes.GenCommittee(rng, 1)
	self := &testNodeCfg{
		validatorKey: validatorKeys[0],
		nodeKey:      makeKey(rng),
		addr:         tcp.TestReserveAddr(),
	}
	for _, tc := range []struct {
		name     string
		hostPort tcp.HostPort
		want     string
	}{
		{"zero port", tcp.HostPort{Hostname: "validator1.example.com"}, "missing port"},
		{"empty hostname", tcp.HostPort{Port: 26656}, "missing hostname"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr := self.GigaNodeAddr()
			addr.HostPort = tc.hostPort
			_, err := NewGigaValidatorRouter(&GigaValidatorConfig{
				GigaRouterCommonConfig: GigaRouterCommonConfig{
					ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
						self.validatorKey.Public(): addr,
					},
				},
				ValidatorKey: self.validatorKey,
			}, self.nodeKey, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

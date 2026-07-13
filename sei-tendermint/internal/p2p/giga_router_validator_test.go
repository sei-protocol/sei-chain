package p2p

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
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
		var allTxs [][]byte
		for i, cfg := range cfgs {
			nodeInfo := makeInfo(cfg.nodeKey)
			nodeInfo.ListenAddr = cfg.addr.String()
			nodeInfo.Network = genDoc.ChainID
			e := Endpoint{AddrPort: cfg.addr}
			app := newTestApp()
			proxyApp := proxy.New(app)
			// In giga mode the CometBFT handshaker is skipped; the router's
			// runExecute calls InitChain itself on fresh start.
			giga, err := NewGigaValidatorRouter(&GigaValidatorConfig{
				GigaRouterCommonConfig: GigaRouterCommonConfig{
					// Aggressive dialing rate to speed up startup.
					DialInterval:       100 * time.Millisecond,
					ValidatorAddrs:     addrs,
					PersistentStateDir: utils.Some(t.TempDir()),
					App:                proxyApp,
					GenDoc:             genDoc,
				},
				ValidatorKey: cfg.validatorKey,
				ViewTimeout:  func(atypes.View) time.Duration { return time.Hour },
				Producer: &producer.Config{
					MaxGasWantedPerBlock:    txGasUsed * maxTxsPerBlock,
					MaxGasEstimatedPerBlock: txGasUsed * maxTxsPerBlock,
					MaxTxsPerBlock:          maxTxsPerBlock,
					MaxTxsPerSecond:         utils.None[uint64](),
					BlockInterval:           100 * time.Millisecond,
					AllowEmptyBlocks:        false,
				},
			}, cfg.nodeKey)
			require.NoError(t, err, "NewGigaValidatorRouter[%v]", i)
			router, err := NewRouter(
				cfg.nodeKey,
				func() *types.NodeInfo { return &nodeInfo },
				dbm.NewMemDB(),
				&RouterOptions{
					SelfAddress:              utils.Some(e.NodeAddress(cfg.nodeKey.Public().NodeID())),
					Endpoint:                 e,
					Connection:               conn.DefaultMConnConfig(),
					IncomingConnectionWindow: utils.Some(time.Duration(0)),
					MaxAcceptRate:            utils.Some(rate.Inf),
					MaxDialRate:              utils.Some(rate.Limit(30)),
					Giga:                     utils.Some[GigaRouter](giga),
				},
			)
			require.NoError(t, err, "NewRouter[%v]", i)
			s.SpawnBgNamed(fmt.Sprintf("router[%v]", i), func() error { return utils.IgnoreCancel(router.Run(ctx)) })
			s.SpawnBgNamed(fmt.Sprintf("giga[%v]", i), func() error { return utils.IgnoreCancel(giga.Run(ctx)) })
			apps = append(apps, app)
			gigas = append(gigas, giga)
			var txs [][]byte
			for range maxTxsPerBlock * blocksPerLane {
				tx := utils.GenBytes(rng, 100)
				txs = append(txs, tx)
				allTxs = append(allTxs, tx)
			}
			s.SpawnNamed(fmt.Sprintf("producer[%v]", i), func() error {
				v := giga.Mempool().OrPanic("validator giga must have a mempool")
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
				require.NoError(t, app.WaitForTx(ctx, tx), "WaitForTx")
			}
		}
		// Nodes should agree on the final state.
		want := apps[0].Snapshot()
		for i, app := range apps {
			t.Logf("app[%v]", i)
			require.NoError(t, utils.TestDiff(want, app.Snapshot()), "state mismatch app[%v]", i)
		}
		// Covers GigaRouter.LastCommittedBlockNumber() — after blocks have
		// been finalized every node should report a non-zero
		// consensus-committed height through the accessor used by /status.
		for i, giga := range gigas {
			committed := giga.LastCommittedBlockNumber()
			require.Positive(t, committed, "router[%v].LastCommittedBlockNumber()", i)
			// Covers GigaRouter.BlockByNumber — the accessor used by the
			// Autobahn branch in env.Block to serve /block and evmrpc block
			// lookups. Fetch the last committed block and verify it carries
			// the expected height + hash, the right chain id, and that the
			// payload Txs round-tripped (we just submitted txs).
			rb, err := giga.BlockByNumber(ctx, atypes.GlobalBlockNumber(committed)) //nolint:gosec // committed is positive (validated above)
			require.NoError(t, err, "router[%v].BlockByNumber(%v)", i, committed)
			require.NotNil(t, rb.Block, "router[%v].BlockByNumber(%v).Block", i, committed)
			require.Equal(t, committed, rb.Block.Height, "router[%v].BlockByNumber(%v) height", i, committed)
			require.NotEmpty(t, rb.BlockID.Hash, "router[%v].BlockByNumber(%v) block hash", i, committed)
			require.Equal(t, genDoc.ChainID, rb.Block.Header.ChainID, "router[%v].BlockByNumber(%v) chain id", i, committed)
			// LastCommit is non-nil with empty Signatures — mirrors
			// executeBlock's FinalizeBlock(DecidedLastCommit: empty)
			// so trace replay and production both see "no votes" on
			// the prior block. ToReqBeginBlock skips the per-val loop
			// when Signatures is empty, so this is also enough to
			// avoid the OOB deref the original PR was guarding against.
			require.NotNil(t, rb.Block.LastCommit, "router[%v].BlockByNumber(%v) LastCommit", i, committed)
			require.Empty(t, rb.Block.LastCommit.Signatures, "router[%v].BlockByNumber(%v) Signatures", i, committed)
			// Round-trip the just-fetched block hash back through
			// BlockByHash and assert we get the same ResultBlock back.
			var hashKey atypes.BlockHeaderHash
			copy(hashKey[:], rb.BlockID.Hash)
			rbh, err := giga.BlockByHash(ctx, hashKey)
			require.NoError(t, err, "router[%v].BlockByHash(%x)", i, rb.BlockID.Hash)
			require.Equal(t, rb, rbh, "router[%v].BlockByHash(%x) ≠ BlockByNumber(%v)", i, rb.BlockID.Hash, committed)
		}
		// Payload.Txs round-trips: for every retained block, the txs the
		// data layer holds (GlobalBlock.Payload.Txs) must equal the txs
		// surfaced through BlockByNumber. Iterates the full retain window
		// rather than a fixed tail so the assertion holds regardless of
		// where producers placed the test txs. Reaches into giga0.data
		// directly — internal same-package access.
		giga0 := gigas[0]
		latest := giga0.LastCommittedBlockNumber()
		for h := int64(1); h <= latest; h++ {
			gbn := atypes.GlobalBlockNumber(h) //nolint:gosec // h is positive
			gb, err := giga0.data.GlobalBlock(ctx, gbn)
			if err != nil {
				continue // pruned out of the retain window
			}
			rb, err := giga0.BlockByNumber(ctx, gbn)
			require.NoError(t, err, "router[0].BlockByNumber(%v)", h)
			// Convert rb.Block.Data.Txs ([]types.Tx) back to [][]byte
			// to compare against gb.Payload.Txs() directly.
			rbBytes := make([][]byte, len(rb.Block.Data.Txs))
			for j, t := range rb.Block.Data.Txs {
				rbBytes[j] = t
			}
			require.Equal(t, gb.Payload.Txs(), rbBytes, "router[0].BlockByNumber(%v).Block.Data.Txs ≠ data.GlobalBlock(%v).Payload.Txs", h, h)
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
	urlByValidator := map[atypes.PublicKey]*url.URL{}
	// NewGigaRouter requires EVMRPC on every committee member in both
	// validator and fullnode modes; the missing-URL branch of EvmProxy is
	// unreachable.
	for i, validatorKey := range validatorKeys {
		nodeKey := makeKey(rng)
		nodeKeys = append(nodeKeys, nodeKey)
		rpcURL, err := url.Parse(fmt.Sprintf("http://validator-%d.example.com:8545", i))
		require.NoError(t, err)
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

	router, err := NewGigaValidatorRouter(&GigaValidatorConfig{
		GigaRouterCommonConfig: GigaRouterCommonConfig{
			DialInterval:       time.Second,
			ValidatorAddrs:     addrs,
			PersistentStateDir: utils.Some(t.TempDir()),
			App:                proxy.New(newTestApp()),
			GenDoc:             genDoc,
		},
		ValidatorKey: validatorKeys[0],
		ViewTimeout:  func(atypes.View) time.Duration { return time.Second },
		Producer: &producer.Config{
			MaxGasWantedPerBlock:    1,
			MaxGasEstimatedPerBlock: 1,
			MaxTxsPerBlock:          1,
			MaxTxsPerSecond:         utils.None[uint64](),
			BlockInterval:           time.Second,
		},
	}, nodeKeys[0])
	require.NoError(t, err)

	localValidator := validatorKeys[0].Public()
	localURL, ok := urlByValidator[localValidator]
	require.True(t, ok)

	expectedRemoteURLs := map[string]struct{}{}
	for validator, rpcURL := range urlByValidator {
		if validator == localValidator {
			continue
		}
		expectedRemoteURLs[rpcURL.String()] = struct{}{}
	}
	returnedRemoteURLs := map[string]struct{}{}

	for range 200 {
		sender := common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		shardValidator := router.data.Registry().LatestEpoch().Committee().EvmShard(sender)

		proxyURL, ok := router.EvmProxy(sender).Get()
		expectedURL := urlByValidator[shardValidator]

		if shardValidator == localValidator {
			// Self-shard: validator short-circuits to local mempool.
			require.False(t, ok)
			require.Nil(t, proxyURL)
		} else {
			require.True(t, ok)
			require.NotNil(t, proxyURL)
			require.Equal(t, expectedURL.String(), proxyURL.String())
			require.NotEqual(t, localURL.String(), proxyURL.String())
			returnedRemoteURLs[proxyURL.String()] = struct{}{}
		}
	}

	require.Equal(t, expectedRemoteURLs, returnedRemoteURLs)
}

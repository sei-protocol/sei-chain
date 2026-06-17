package p2p

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type shaHash = [sha256.Size]byte

type testAppState struct {
	Init       utils.Option[*abci.RequestInitChain]
	Validators []abci.ValidatorUpdate
	Blocks     []*abci.RequestFinalizeBlock
	Txs        map[shaHash]bool
	AppHash    shaHash
	// Committed tracks whether FinalizeBlock is allowed.
	// Set to true by InitChain (so FinalizeBlock can follow without Commit,
	// matching the CometBFT handshaker flow) and by Commit.
	// Cleared by FinalizeBlock.
	Committed bool
}

func testAppStateJSON(rng utils.Rng) json.RawMessage {
	return utils.OrPanic1(json.Marshal(&abci.ValidatorUpdate{
		PubKey: crypto.PubKeyToProto(ed25519.TestSecretKey(utils.GenBytes(rng, 32)).Public()),
		Power:  rng.Int63(),
	}))
}

type testApp struct {
	abci.BaseApplication
	state utils.Watch[*testAppState]
}

func newTestApp() *testApp {
	return &testApp{state: utils.NewWatch(&testAppState{
		Txs: map[shaHash]bool{},
	})}
}

func (a *testApp) GetValidators() []abci.ValidatorUpdate {
	for state := range a.state.Lock() {
		return slices.Clone(state.Validators)
	}
	panic("unreachable")
}

func (a *testApp) Info(_ context.Context, _ *abci.RequestInfo) (*abci.ResponseInfo, error) {
	for state := range a.state.Lock() {
		init, ok := state.Init.Get()
		if !ok {
			return &abci.ResponseInfo{}, nil
		}
		if len(state.Blocks) == 0 {
			// Match the real SDK: InitChain without Commit leaves LastBlockHeight=0.
			return &abci.ResponseInfo{
				LastBlockHeight:  0,
				LastBlockAppHash: slices.Clone(state.AppHash[:]),
			}, nil
		}
		return &abci.ResponseInfo{
			LastBlockHeight:  init.InitialHeight + int64(len(state.Blocks)) - 1,
			LastBlockAppHash: slices.Clone(state.AppHash[:]),
		}, nil
	}
	panic("unreachable")
}

func (a *testApp) CheckTx(context.Context, *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    1,
			GasEstimated: 1,
		},
	}
}

func (a *testApp) InitChain(_ context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	for state, ctrl := range a.state.Lock() {
		if state.Init.IsPresent() {
			return nil, fmt.Errorf("chain already initialized")
		}
		if req.InitialHeight < 1 {
			return nil, fmt.Errorf("InitialHeight = %v, want >=1", req.InitialHeight)
		}
		var val abci.ValidatorUpdate
		if err := json.Unmarshal(req.AppStateBytes, &val); err != nil {
			return nil, fmt.Errorf("proto.Unmarshal(): %w", err)
		}
		state.Init = utils.Some(req)
		state.AppHash = sha256.Sum256(req.AppStateBytes)
		state.Validators = utils.Slice(val)
		state.Committed = true
		ctrl.Updated()
		return &abci.ResponseInitChain{
			AppHash:    slices.Clone(state.AppHash[:]),
			Validators: slices.Clone(state.Validators),
		}, nil
	}
	panic("unreachable")
}

func (a *testApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	for state, ctrl := range a.state.Lock() {
		if !state.Committed {
			return nil, fmt.Errorf("FinalizeBlock before Commit")
		}
		init, ok := state.Init.Get()
		if !ok {
			return nil, fmt.Errorf("app not initialized")
		}
		state.Blocks = append(state.Blocks, req)
		state.AppHash = sha256.Sum256(slices.Concat(req.Hash, state.AppHash[:]))
		for _, tx := range req.Txs {
			state.Txs[sha256.Sum256(tx)] = true
		}
		logger.Info("FinalizeBlock", "n", req.Header.Height-init.InitialHeight)
		state.Committed = false
		ctrl.Updated()
		return &abci.ResponseFinalizeBlock{
			AppHash:   slices.Clone(state.AppHash[:]),
			TxResults: slices.Repeat([]*abci.ExecTxResult{{Code: abci.CodeTypeOK}}, len(req.Txs)),
		}, nil
	}
	panic("unreachable")
}

func (a *testApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state, ctrl := range a.state.Lock() {
		if state.Committed {
			return nil, fmt.Errorf("double commit")
		}
		state.Committed = true
		ctrl.Updated()
	}
	return &abci.ResponseCommit{
		// Don't prune anything.
		RetainHeight: 0,
	}, nil
}

func (a *testApp) WaitForTx(ctx context.Context, tx []byte) error {
	h := sha256.Sum256(tx)
	for state, ctrl := range a.state.Lock() {
		return ctrl.WaitUntil(ctx, func() bool {
			_, ok := state.Txs[h]
			return ok
		})
	}
	panic("unreachable")
}

func (a *testApp) Snapshot() testAppState {
	for state := range a.state.Lock() {
		s := *state
		// Txs is derived and Committed is not deterministic.
		s.Txs = nil
		s.Committed = false
		return s
	}
	panic("unreachable")
}

type testNodeCfg struct {
	validatorKey atypes.SecretKey
	nodeKey      NodeSecretKey
	addr         netip.AddrPort
}

func (c *testNodeCfg) GigaNodeAddr() GigaNodeAddr {
	// EVMRPC must be present for NewGigaRouter to accept the config on
	// either path; the URL value is unused by the tests in this file.
	u, err := url.Parse(fmt.Sprintf("http://%s:8545", c.addr.Addr().String()))
	if err != nil {
		panic(err)
	}
	return GigaNodeAddr{
		Key:      c.nodeKey.Public(),
		HostPort: tcp.HostPort{Hostname: c.addr.Addr().String(), Port: c.addr.Port()},
		EVMRPC:   utils.Some(u),
	}
}

// TestInitChainCommitThenFinalize is a contract test for testApp: it verifies
// that testApp supports the autobahn block execution flow where runExecute
// calls InitChain (no Commit), then FinalizeBlock at InitialHeight using the
// deliverState set up by InitChain, followed by Commit.
func TestInitChainCommitThenFinalize(t *testing.T) {
	rng := utils.TestRng()
	app := newTestApp()
	ctx := t.Context()

	initialHeight := rng.Int63n(100000) + 1
	appState := testAppStateJSON(rng)

	// InitChain
	_, err := app.InitChain(ctx, &abci.RequestInitChain{
		InitialHeight: initialHeight,
		AppStateBytes: appState,
	})
	require.NoError(t, err)

	// No Commit after InitChain — the SDK expects FinalizeBlock at InitialHeight
	// using the deliverState set up by InitChain.

	// Verify app reports correct height after InitChain (no blocks yet)
	info, err := app.Info(ctx, &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, int64(0), info.LastBlockHeight,
		"testApp should report 0 after InitChain with no committed blocks (matches real SDK)")

	// FinalizeBlock should succeed — deliverState was set up by InitChain
	blockHash := sha256.Sum256([]byte("test-block"))
	_, err = app.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{
		Hash: blockHash[:],
		Header: (&types.Header{
			Height: initialHeight,
		}).ToProto(),
	})
	require.NoError(t, err)

	// Second Commit should succeed
	_, err = app.Commit(ctx)
	require.NoError(t, err)

	// Verify height advanced
	info, err = app.Info(ctx, &abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, initialHeight, info.LastBlockHeight,
		"testApp should report InitialHeight after 1 block")
}

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
		var routers []*Router
		var allTxs [][]byte
		for i, cfg := range cfgs {
			nodeInfo := makeInfo(cfg.nodeKey)
			nodeInfo.ListenAddr = cfg.addr.String()
			nodeInfo.Network = genDoc.ChainID
			e := Endpoint{AddrPort: cfg.addr}
			app := newTestApp()
			proxyApp := proxy.New(app, proxy.NopMetrics())
			// In giga mode the CometBFT handshaker is skipped; the router's
			// runExecute calls InitChain itself on fresh start.
			giga, err := NewGigaValidatorRouter(&GigaValidatorConfig{
				GigaRouterCommonConfig: GigaRouterCommonConfig{
					// Aggressive dialing rate to speed up startup.
					DialInterval:       100 * time.Millisecond,
					ValidatorAddrs:     addrs,
					PersistentStateDir: utils.None[string](),
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
				NopMetrics(),
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
			apps = append(apps, app)
			routers = append(routers, router)
			var txs [][]byte
			for range maxTxsPerBlock * blocksPerLane {
				tx := utils.GenBytes(rng, 100)
				txs = append(txs, tx)
				allTxs = append(allTxs, tx)
			}
			s.SpawnNamed(fmt.Sprintf("producer[%v]", i), func() error {
				giga := router.Giga().OrPanic("non-giga router")
				v := giga.AsValidator().OrPanic("validator giga must satisfy GigaValidatorRouter")
				for _, tx := range txs {
					if _, err := v.Mempool().InsertTx(ctx, tx); err != nil {
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
		// Covers Router.Giga() + GigaRouter.LastCommittedBlockNumber() — after
		// blocks have been finalized every node should report a non-zero
		// consensus-committed height through the new accessors used by /status.
		for i, r := range routers {
			giga := r.Giga().OrPanic("non-giga router")
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
		// where producers placed the test txs.
		giga0, _ := routers[0].Giga().Get()
		latest := giga0.LastCommittedBlockNumber()
		// Reach into the concrete validator impl to compare against
		// data.State directly; this is an internal same-package test.
		giga0Val := giga0.(*gigaValidatorRouter)
		for h := int64(1); h <= latest; h++ {
			gbn := atypes.GlobalBlockNumber(h) //nolint:gosec // h is positive
			gb, err := giga0Val.data.GlobalBlock(ctx, gbn)
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
			EVMRPC:   utils.Some(rpcURL),
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
			PersistentStateDir: utils.None[string](),
			App:                proxy.New(newTestApp(), proxy.NopMetrics()),
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
		shardValidator := router.data.Committee().EvmShard(sender)

		proxyURL, ok := router.EvmProxy(sender)
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

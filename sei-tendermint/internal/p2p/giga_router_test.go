package p2p

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
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
	abci.Application
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

func (a *testApp) CheckTx(context.Context, *abci.RequestCheckTxV2) (*abci.ResponseCheckTxV2, error) {
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:      abci.CodeTypeOK,
			GasWanted: 1,
		},
	}, nil
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
	return GigaNodeAddr{
		Key:      c.nodeKey.Public(),
		HostPort: tcp.HostPort{Hostname: c.addr.Addr().String(), Port: c.addr.Port()},
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
			txMempool := mempool.NewTxMempool(mempool.TestConfig(), proxyApp, mempool.NopMetrics(), mempool.NopTxConstraintsFetcher)
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
					Giga: utils.Some(&GigaRouterConfig{
						// Aggressive dialing rate to speed up startup.
						DialInterval:   100 * time.Millisecond,
						ValidatorAddrs: addrs,
						Consensus: &consensus.Config{
							Key:                cfg.validatorKey,
							ViewTimeout:        func(atypes.View) time.Duration { return time.Hour },
							PersistentStateDir: utils.None[string](),
						},
						Producer: &producer.Config{
							MaxGasPerBlock:   txGasUsed * maxTxsPerBlock,
							MaxTxsPerBlock:   maxTxsPerBlock,
							MaxTxsPerSecond:  utils.None[uint64](),
							MempoolSize:      100,
							BlockInterval:    100 * time.Millisecond,
							AllowEmptyBlocks: false,
						},
						TxMempool: txMempool,
						GenDoc:    genDoc,
					}),
				},
			)
			if err != nil {
				return fmt.Errorf("NewRouter(): %w", err)
			}
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
				for _, payload := range txs {
					if _, err := txMempool.CheckTx(ctx, payload, mempool.TxInfo{}); err != nil {
						return fmt.Errorf("txMempool.CheckTx(): %w", err)
					}
				}
				return nil
			})
		}
		// Each node should finalize all txs locally.
		for _, app := range apps {
			for _, tx := range allTxs {
				if err := app.WaitForTx(ctx, tx); err != nil {
					return fmt.Errorf("WaitForTx(): %w", err)
				}
			}
		}
		// Nodes should agree on the final state.
		want := apps[0].Snapshot()
		for i, app := range apps {
			t.Logf("app[%v]", i)
			if err := utils.TestDiff(want, app.Snapshot()); err != nil {
				return fmt.Errorf("state mismatch: %w", err)
			}
		}
		// Covers Router.Giga() + GigaRouter.LastCommittedBlockNumber() — after
		// blocks have been finalized every node should report a non-zero
		// consensus-committed height through the new accessors used by /status.
		for i, r := range routers {
			giga, ok := r.Giga().Get()
			if !ok {
				return fmt.Errorf("router[%v].Giga(): none, want some", i)
			}
			committed := giga.LastCommittedBlockNumber()
			if committed <= 0 {
				return fmt.Errorf("router[%v].LastCommittedBlockNumber() = %v, want > 0", i, committed)
			}
			// Covers GigaRouter.BlockByNumber — the accessor used by the
			// Autobahn branch in env.Block to serve /block and evmrpc block
			// lookups. Fetch the last committed block and verify it carries
			// the expected height + hash, the right chain id, and that the
			// payload Txs round-tripped (we just submitted txs).
			rb, err := giga.BlockByNumber(ctx, committed)
			if err != nil {
				return fmt.Errorf("router[%v].BlockByNumber(%v): %w", i, committed, err)
			}
			if rb == nil || rb.Block == nil {
				return fmt.Errorf("router[%v].BlockByNumber(%v): nil block", i, committed)
			}
			if rb.Block.Height != committed {
				return fmt.Errorf("router[%v].BlockByNumber(%v): got height %v", i, committed, rb.Block.Height)
			}
			if len(rb.BlockID.Hash) == 0 {
				return fmt.Errorf("router[%v].BlockByNumber(%v): empty block hash", i, committed)
			}
			if rb.Block.Header.ChainID != genDoc.ChainID {
				return fmt.Errorf("router[%v].BlockByNumber(%v): chain id %q, want %q",
					i, committed, rb.Block.Header.ChainID, genDoc.ChainID)
			}
			// Round-trip the just-fetched block hash back through
			// BlockByHash and assert we land on the same height + bytes.
			var hashKey atypes.BlockHeaderHash
			copy(hashKey[:], rb.BlockID.Hash)
			rbh, err := giga.BlockByHash(ctx, hashKey)
			if err != nil {
				return fmt.Errorf("router[%v].BlockByHash(%x): %w", i, rb.BlockID.Hash, err)
			}
			if rbh == nil || rbh.Block == nil {
				return fmt.Errorf("router[%v].BlockByHash(%x): nil block", i, rb.BlockID.Hash)
			}
			if rbh.Block.Height != committed {
				return fmt.Errorf("router[%v].BlockByHash(%x): got height %v, want %v",
					i, rb.BlockID.Hash, rbh.Block.Height, committed)
			}
			if !bytes.Equal(rbh.BlockID.Hash, rb.BlockID.Hash) {
				return fmt.Errorf("router[%v].BlockByHash(%x): got hash %x, want round-trip",
					i, rb.BlockID.Hash, rbh.BlockID.Hash)
			}
		}
		// At least one of the global blocks we just finalized must carry txs
		// — the producers fed maxTxsPerBlock*blocksPerLane txs into the
		// mempool. Walk a small range from the latest backwards and assert
		// we saw at least one non-empty payload, exercising the
		// Payload.Txs round-trip end-to-end.
		giga0, _ := routers[0].Giga().Get()
		latest := giga0.LastCommittedBlockNumber()
		sawTxs := false
		for h := latest; h > 0 && h > latest-int64(blocksPerLane*len(cfgs)); h-- {
			rb, err := giga0.BlockByNumber(ctx, h)
			if err != nil {
				return fmt.Errorf("router[0].BlockByNumber(%v): %w", h, err)
			}
			if rb != nil && rb.Block != nil && len(rb.Block.Data.Txs) > 0 {
				sawTxs = true
				break
			}
		}
		if !sawTxs {
			return fmt.Errorf("router[0].BlockByNumber: no non-empty payload found in last %v blocks", blocksPerLane*len(cfgs))
		}
		return nil
	})
	require.NoError(t, err)
}

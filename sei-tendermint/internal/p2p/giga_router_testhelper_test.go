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

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

type shaHash = [sha256.Size]byte

type testAppState struct {
	Init       utils.Option[*proxy.RequestInitChain]
	Validators []proxy.ValidatorUpdate
	Blocks     []*proxy.RequestFinalizeBlock
	Txs        map[shaHash]bool
	AppHash    shaHash
	// Committed tracks whether FinalizeBlock is allowed.
	// Set to true by InitChain (so FinalizeBlock can follow without Commit,
	// matching the CometBFT handshaker flow) and by Commit.
	// Cleared by FinalizeBlock.
	Committed bool
}

func testAppStateJSON(rng utils.Rng) json.RawMessage {
	return utils.OrPanic1(json.Marshal(&proxy.ValidatorUpdate{
		PubKey: atypes.GenSecretKey(rng).Public(),
		Power:  rng.Int63(),
	}))
}

type testApp struct {
	proxy.EVMApplication
	state utils.Watch[*testAppState]
}

func newTestApp() *testApp {
	return &testApp{
		state: utils.NewWatch(&testAppState{
			Txs: map[shaHash]bool{},
		}),
	}
}

func (a *testApp) EvmNonce(common.Address) uint64 { return 0 }

func (a *testApp) GetValidators() []proxy.ValidatorUpdate {
	for state := range a.state.Lock() {
		return slices.Clone(state.Validators)
	}
	panic("unreachable")
}

func (a *testApp) Info() *proxy.ResponseInfo {
	for state := range a.state.Lock() {
		init, ok := state.Init.Get()
		if !ok {
			return &proxy.ResponseInfo{}
		}
		if len(state.Blocks) == 0 {
			// Match the real SDK: InitChain without Commit leaves LastBlockHeight=0.
			return &proxy.ResponseInfo{
				LastBlockHeight:  0,
				LastBlockAppHash: slices.Clone(state.AppHash[:]),
			}
		}
		return &proxy.ResponseInfo{
			LastBlockHeight:  init.InitialHeight + int64(len(state.Blocks)) - 1,
			LastBlockAppHash: slices.Clone(state.AppHash[:]),
		}
	}
	panic("unreachable")
}

func (a *testApp) LastBlockHeight() int64 {
	for state := range a.state.Lock() {
		init, ok := state.Init.Get()
		if !ok || len(state.Blocks) == 0 {
			return 0
		}
		return init.InitialHeight + int64(len(state.Blocks)) - 1
	}
	panic("unreachable")
}

func (a *testApp) CheckTx(_ context.Context, req *proxy.RequestCheckTxV2) *proxy.ResponseCheckTxV2 {
	hash := sha256.Sum256(req.Tx)
	return &proxy.ResponseCheckTxV2{
		// Each mock transaction has its own sender and uses nonce zero.
		IsEVM:            true,
		EVMSenderAddress: common.Address(hash[:20]),
		SeiSenderAddress: hash[:20],
		EVMHash:          common.Hash(hash),
		ResponseCheckTx: &proxy.ResponseCheckTx{
			Code:         proxy.CodeTypeOK,
			GasWanted:    1,
			GasEstimated: 1,
		},
	}
}

func (a *testApp) InitChain(req *proxy.RequestInitChain) (*proxy.ResponseInitChain, error) {
	for state, ctrl := range a.state.Lock() {
		if state.Init.IsPresent() {
			return nil, fmt.Errorf("chain already initialized")
		}
		if req.InitialHeight < 1 {
			return nil, fmt.Errorf("InitialHeight = %v, want >=1", req.InitialHeight)
		}
		var val proxy.ValidatorUpdate
		if err := json.Unmarshal(req.AppStateBytes, &val); err != nil {
			return nil, fmt.Errorf("json.Unmarshal(): %w", err)
		}
		state.Init = utils.Some(req)
		state.AppHash = sha256.Sum256(req.AppStateBytes)
		state.Validators = utils.Slice(val)
		state.Committed = true
		ctrl.Updated()
		return &proxy.ResponseInitChain{
			AppHash:    slices.Clone(state.AppHash[:]),
			Validators: slices.Clone(state.Validators),
		}, nil
	}
	panic("unreachable")
}

func (a *testApp) FinalizeBlock(ctx context.Context, req *proxy.RequestFinalizeBlock) (*proxy.ResponseFinalizeBlock, error) {
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
		return &proxy.ResponseFinalizeBlock{
			AppHash:   slices.Clone(state.AppHash[:]),
			TxResults: slices.Repeat([]*proxy.ExecTxResult{{Code: proxy.CodeTypeOK}}, len(req.Txs)),
		}, nil
	}
	panic("unreachable")
}

func (a *testApp) Commit(context.Context) (*proxy.ResponseCommit, error) {
	for state, ctrl := range a.state.Lock() {
		if state.Committed {
			return nil, fmt.Errorf("double commit")
		}
		state.Committed = true
		ctrl.Updated()
	}
	return &proxy.ResponseCommit{}, nil
}

// WaitForBlocks waits until the app has finalized at least count blocks and
// reports how many it had finalized at that moment.
func (a *testApp) WaitForBlocks(ctx context.Context, count int) (int, error) {
	for state, ctrl := range a.state.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return len(state.Blocks) >= count }); err != nil {
			return 0, err
		}
		return len(state.Blocks), nil
	}
	panic("unreachable")
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
	return GigaNodeAddr{
		Key:      c.nodeKey.Public(),
		HostPort: tcp.HostPort{Hostname: c.addr.Addr().String(), Port: c.addr.Port()},
		EVMRPC:   *utils.OrPanic1(url.Parse(fmt.Sprintf("ws://%s:8546", c.addr.Addr().String()))),
	}
}

// TestInitChainCommitThenFinalize is a contract test for testApp: it verifies
// that testApp supports the autobahn block execution flow where the executor
// calls InitChain (no Commit), then FinalizeBlock at InitialHeight using the
// deliverState set up by InitChain, followed by Commit.
func TestInitChainCommitThenFinalize(t *testing.T) {
	rng := utils.TestRng()
	app := newTestApp()
	ctx := t.Context()

	initialHeight := rng.Int63n(100000) + 1
	appState := testAppStateJSON(rng)

	// InitChain
	_, err := app.InitChain(&proxy.RequestInitChain{
		InitialHeight: initialHeight,
		AppStateBytes: appState,
	})
	require.NoError(t, err)

	// No Commit after InitChain — the SDK expects FinalizeBlock at InitialHeight
	// using the deliverState set up by InitChain.

	// Verify app reports correct height after InitChain (no blocks yet)
	info := app.Info()
	require.Equal(t, int64(0), info.LastBlockHeight,
		"testApp should report 0 after InitChain with no committed blocks (matches real SDK)")

	// FinalizeBlock should succeed — deliverState was set up by InitChain
	blockHash := sha256.Sum256([]byte("test-block"))
	_, err = app.FinalizeBlock(ctx, &proxy.RequestFinalizeBlock{
		Hash:   blockHash[:],
		Header: &proxy.Header{Height: initialHeight},
	})
	require.NoError(t, err)

	// Second Commit should succeed
	_, err = app.Commit(ctx)
	require.NoError(t, err)

	// Verify height advanced
	info = app.Info()
	require.Equal(t, initialHeight, info.LastBlockHeight,
		"testApp should report InitialHeight after 1 block")
}

// newTestExecutor returns an executor of cfg's app over the blocks store makes
// durable.
func newTestExecutor(cfg *GigaRouterCommonConfig, store *blockstore.Store) *execution.Executor {
	return execution.New(&execution.Config{
		GenDoc:         cfg.GenDoc,
		App:            cfg.App,
		AppHashBuilder: cfg.AppHashBuilder,
		BlockStore:     store,
	})
}

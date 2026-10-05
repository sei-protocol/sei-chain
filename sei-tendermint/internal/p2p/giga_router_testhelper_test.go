package p2p

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
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

func (a *testApp) Info() *abci.ResponseInfo {
	for state := range a.state.Lock() {
		init, ok := state.Init.Get()
		if !ok {
			return &abci.ResponseInfo{}
		}
		if len(state.Blocks) == 0 {
			// Match the real SDK: InitChain without Commit leaves LastBlockHeight=0.
			return &abci.ResponseInfo{
				LastBlockHeight:  0,
				LastBlockAppHash: slices.Clone(state.AppHash[:]),
			}
		}
		return &abci.ResponseInfo{
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

func (a *testApp) CheckTx(context.Context, *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    1,
			GasEstimated: 1,
		},
	}
}

func (a *testApp) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
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

// CheckBlocks verifies the FinalizeBlock requests the router delivered: heights
// are contiguous from InitialHeight and every header names a current validator
// as proposer.
func (s *testAppState) CheckBlocks() error {
	init, ok := s.Init.Get()
	if !ok {
		return fmt.Errorf("app not initialized")
	}
	for i, b := range s.Blocks {
		if want := init.InitialHeight + int64(i); b.Header.Height != want {
			return fmt.Errorf("blocks[%v].Height = %v, want %v", i, b.Header.Height, want)
		}
		if err := checkProposer(b.Header.ProposerAddress, s.Validators); err != nil {
			return fmt.Errorf("blocks[%v]: %w", i, err)
		}
	}
	return nil
}

// checkProposer verifies that the proposer is one of the given validators.
func checkProposer(proposer types.Address, vals []abci.ValidatorUpdate) error {
	for _, val := range vals {
		key, err := crypto.PubKeyFromProto(val.PubKey)
		if err != nil {
			return fmt.Errorf("crypto.PubKeyFromProto(): %w", err)
		}
		if bytes.Equal(key.Address(), proposer) {
			return nil
		}
	}
	return fmt.Errorf("proposer %X is not a current validator", proposer)
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
	return GigaNodeAddr{
		Key:      c.nodeKey.Public(),
		HostPort: tcp.HostPort{Hostname: c.addr.Addr().String(), Port: c.addr.Port()},
		EVMRPC:   *utils.OrPanic1(url.Parse(fmt.Sprintf("http://%s:8545", c.addr.Addr().String()))),
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
	_, err := app.InitChain(&abci.RequestInitChain{
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
	info = app.Info()
	require.Equal(t, initialHeight, info.LastBlockHeight,
		"testApp should report InitialHeight after 1 block")
}

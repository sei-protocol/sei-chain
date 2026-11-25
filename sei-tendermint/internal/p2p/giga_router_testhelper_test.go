package p2p

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net/netip"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type shaHash = [sha256.Size]byte

// testAppBlock records block inputs without process-local transaction caches.
type testAppBlock struct {
	Height atypes.RoadHeight
	Time   time.Time
	Hash   common.Hash
	Txs    [][]byte
}

type testAppState struct {
	Init       utils.Option[abevm.InitConfig]
	Validators abevm.CommitteeWeights
	Blocks     []testAppBlock
	Txs        map[shaHash]bool
	AppHash    shaHash
}

func testAppStateJSON(rng utils.Rng) json.RawMessage {
	weights := abevm.CommitteeWeights{atypes.GenSecretKey(rng).Public(): rng.Uint64() >> 1}
	return utils.OrPanic1(json.Marshal(weights))
}

type testApp struct {
	abevm.Application
	state utils.Watch[*testAppState]
}

func newTestApp(genesis []byte) *testApp {
	var weights abevm.CommitteeWeights
	utils.OrPanic(json.Unmarshal(genesis, &weights))
	return &testApp{
		state: utils.NewWatch(&testAppState{
			Txs: map[shaHash]bool{}, Validators: weights, AppHash: sha256.Sum256(genesis),
		}),
	}
}

func (a *testApp) EvmChainID() uint64 { return 1 }

func (a *testApp) EvmNonce(common.Address) uint64 { return 0 }

// EvmCommittedNonce matches EvmNonce: the test app has no commit in flight.
func (a *testApp) EvmCommittedNonce(addr common.Address) uint64 { return a.EvmNonce(addr) }

func (a *testApp) GetValidators() abevm.CommitteeWeights {
	for state := range a.state.Lock() {
		return maps.Clone(state.Validators)
	}
	panic("unreachable")
}

func (a *testApp) Info() *abevm.ResponseInfo {
	for state := range a.state.Lock() {
		init, ok := state.Init.Get()
		if !ok {
			return &abevm.ResponseInfo{}
		}
		if len(state.Blocks) == 0 {
			// Initialization alone leaves LastBlockHeight=0.
			return &abevm.ResponseInfo{
				LastBlockHeight:  0,
				LastBlockAppHash: slices.Clone(state.AppHash[:]),
			}
		}
		return &abevm.ResponseInfo{
			LastBlockHeight:  int64(init.InitialHeight) + int64(len(state.Blocks)) - 1,
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
		return int64(init.InitialHeight) + int64(len(state.Blocks)) - 1
	}
	panic("unreachable")
}

func (a *testApp) EvmBlockNumber() int64 {
	return max(0, a.LastBlockHeight())
}

func (a *testApp) BlockTxsHashes(height int64) ([]common.Hash, error) {
	for state := range a.state.Lock() {
		for _, block := range state.Blocks {
			if utils.Clamp[int64](block.Height) != height {
				continue
			}
			hashes := make([]common.Hash, len(block.Txs))
			for i, raw := range block.Txs {
				hashes[i] = crypto.Keccak256Hash(raw)
			}
			return hashes, nil
		}
		return nil, fmt.Errorf("block %d has not executed", height)
	}
	panic("unreachable")
}

func (a *testApp) CheckTx(tx abevm.EvmTx) (abevm.AdmittedTx, error) {
	return abevm.AdmittedTx{GasEstimated: 1}, nil
}

func (a *testApp) InitChain(req abevm.InitConfig) error {
	for state, ctrl := range a.state.Lock() {
		if state.Init.IsPresent() {
			return fmt.Errorf("chain already initialized")
		}
		if req.InitialHeight < 1 {
			return fmt.Errorf("InitialHeight = %v, want >=1", req.InitialHeight)
		}
		state.Init = utils.Some(req)
		ctrl.Updated()
		return nil
	}
	panic("unreachable")
}

func (a *testApp) FinalizeBlock(ctx context.Context, req abevm.FinalizeBlockReq) (abevm.FinalizeBlockResp, error) {
	for state, ctrl := range a.state.Lock() {
		init, ok := state.Init.Get()
		if !ok {
			return abevm.FinalizeBlockResp{}, fmt.Errorf("app not initialized")
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return init.InitialHeight+atypes.RoadHeight(len(state.Blocks)) >= req.Height
		}); err != nil {
			return abevm.FinalizeBlockResp{}, err
		}
		txs := make([][]byte, len(req.Txs))
		for i, tx := range req.Txs {
			txs[i] = utils.OrPanic1(tx.MarshalBinary())
		}
		state.Blocks = append(state.Blocks, testAppBlock{Height: req.Height, Time: req.Time, Hash: req.Hash, Txs: txs})
		state.AppHash = sha256.Sum256(slices.Concat(req.Hash[:], state.AppHash[:]))
		for _, tx := range req.Txs {
			state.Txs[sha256.Sum256(utils.OrPanic1(tx.MarshalBinary()))] = true
		}
		logger.Info("FinalizeBlock", "n", req.Height-init.InitialHeight)
		ctrl.Updated()
		return abevm.FinalizeBlockResp{}, nil
	}
	panic("unreachable")
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
		// Txs is derived.
		s.Txs = nil
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

// TestInitChainThenFinalize verifies initialization followed by finalization at InitialHeight.
func TestInitChainThenFinalize(t *testing.T) {
	rng := utils.TestRng()
	appState := testAppStateJSON(rng)
	app := newTestApp(appState)
	ctx := t.Context()

	initialHeight := rng.Int63n(100000) + 1

	// InitChain
	err := app.InitChain(abevm.InitConfig{
		InitialHeight: atypes.RoadHeight(initialHeight)})
	require.NoError(t, err)

	// Verify app reports correct height after InitChain (no blocks yet)
	info := app.Info()
	require.Equal(t, int64(0), info.LastBlockHeight,
		"testApp should report 0 after InitChain with no finalized blocks")

	// FinalizeBlock should succeed after InitChain
	blockHash := sha256.Sum256([]byte("test-block"))
	_, err = app.FinalizeBlock(ctx, abevm.FinalizeBlockReq{
		Hash:   common.BytesToHash(blockHash[:]),
		Height: atypes.RoadHeight(initialHeight)})
	require.NoError(t, err)

	// Verify height advanced
	info = app.Info()
	require.Equal(t, initialHeight, info.LastBlockHeight,
		"testApp should report InitialHeight after 1 block")
}

// newTestExecutor returns an executor of cfg's app over the blocks store makes
// durable.
func newTestExecutor(cfg *GigaRouterCommonConfig, store *blockstore.Store, signer *abevm.Signer) *execution.Executor {
	return execution.New(&execution.Config{
		GenDoc:            cfg.GenDoc,
		App:               cfg.App,
		AppHashBuilder:    cfg.AppHashBuilder,
		BlockStore:        store,
		Signer:            signer,
		MaxBlocksInFlight: gigaconfig.DefaultConfig.Execution.MaxBlocksInFlight,
	})
}

func testGigaConsensusParams() *tmtypes.ConsensusParams {
	params := tmtypes.DefaultConsensusParams()
	params.Block.MaxGas = 30_000_000
	return params
}

func newTestSigner(chainID uint64, workers int) *abevm.Signer {
	return abevm.NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}

func testEVMRPCURL() url.URL {
	return url.URL{Scheme: "ws", Host: "127.0.0.1:1"}
}

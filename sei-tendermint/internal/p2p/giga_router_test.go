package p2p

import (
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
			giga, ok := r.Giga().Get()
			require.True(t, ok, "router[%v].Giga()", i)
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
			// LastCommit must be populated so trace replay's BeginBlock
			// (which iterates Signatures by index against genDoc.Validators)
			// doesn't nil-deref. We don't assert per-signer flags here —
			// those are unit-tested in TestCommitSigsFromQC — just that
			// the slice is sized to the validator set.
			require.NotNil(t, rb.Block.LastCommit, "router[%v].BlockByNumber(%v) LastCommit", i, committed)
			require.Len(t, rb.Block.LastCommit.Signatures, len(genDoc.Validators), "router[%v].BlockByNumber(%v) Signatures len", i, committed)
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

// TestCommitSigsFromQC covers the LastCommit-shape translation that
// GigaRouter.translateGlobalBlock applies on every Block / BlockByHash
// response. Each subtest builds a fresh test environment so failures
// describe a single case in isolation.
//
// Beyond happy-path full/subset signing, two boundary cases get
// dedicated coverage:
//   - None qc: simulates genesis or pruned-prior; every position must
//     come back Absent with zero Timestamp/empty Signature so that
//     types.CommitSig.ValidateBasic accepts each entry.
//   - QC carries a signer not in genVals: simulates committee drift
//     after a validator-set change. The extra signer is silently
//     dropped, all genVals still resolve to their correct slots, no
//     panic. Catches a subtle alignment-by-iteration bug where an
//     extraneous signer would shift indexes.
func TestCommitSigsFromQC(t *testing.T) {
	rng := utils.TestRng()
	// Build 4 keys; genVals stays in insertion order (NOT sorted by
	// pubkey) so the test catches any code path that incorrectly sorts.
	keys := make([]atypes.SecretKey, 4)
	genVals := make([]types.GenesisValidator, 4)
	for i := range keys {
		keys[i] = atypes.GenSecretKey(rng)
		pub, err := ed25519.PublicKeyFromBytes(keys[i].Public().Bytes())
		require.NoError(t, err)
		genVals[i] = types.GenesisValidator{
			Address: pub.Address(),
			PubKey:  pub,
			Power:   10,
			Name:    fmt.Sprintf("v%d", i),
		}
	}
	ts := time.Unix(1_700_000_000, 0)

	// mkQC returns a CommitQC where each signerSecrets entry signs the
	// same CommitVote, plus the matching []*atypes.Signed slice so the
	// test can pull out exact ed25519 bytes for sig-byte assertions.
	// NewCommitQC requires len > 0, so empty-signer cases are tested
	// through the None path.
	mkQC := func(signerSecrets []atypes.SecretKey) (utils.Option[*atypes.CommitQC], []*atypes.Signed[*atypes.CommitVote]) {
		vote := atypes.GenCommitVote(rng)
		signed := make([]*atypes.Signed[*atypes.CommitVote], len(signerSecrets))
		for i, sk := range signerSecrets {
			signed[i] = atypes.Sign(sk, vote)
		}
		return utils.Some(atypes.NewCommitQC(signed)), signed
	}

	// expectedSigBytes maps the secret key's pubkey-bytes to the raw
	// ed25519 sig bytes the corresponding Signed produced. Lets us
	// assert Signature carries the actual signing material, not just
	// "something non-None".
	sigBytesByKey := func(signed []*atypes.Signed[*atypes.CommitVote]) map[string][]byte {
		m := map[string][]byte{}
		for _, sm := range signed {
			m[string(sm.Sig().Key().Bytes())] = sm.Sig().Sig().Bytes()
		}
		return m
	}

	assertAbsent := func(t *testing.T, sig types.CommitSig, msg string) {
		t.Helper()
		require.Equal(t, types.BlockIDFlagAbsent, sig.BlockIDFlag, "%s: BlockIDFlag", msg)
		require.Empty(t, sig.ValidatorAddress, "%s: address must be empty per ValidateBasic", msg)
		require.True(t, sig.Timestamp.IsZero(), "%s: timestamp must be zero per ValidateBasic", msg)
		require.False(t, sig.Signature.IsPresent(), "%s: signature must be absent", msg)
	}

	t.Run("full committee signs", func(t *testing.T) {
		qc, signed := mkQC(keys)
		want := sigBytesByKey(signed)
		sigs := commitSigsFromQC(genVals, qc, ts)
		require.Len(t, sigs, len(genVals))
		for i, sig := range sigs {
			require.Equal(t, types.BlockIDFlagCommit, sig.BlockIDFlag, "sigs[%d]", i)
			// sigs[i] aligns with genVals[i] (insertion order).
			require.Equal(t, genVals[i].PubKey.Address().Bytes(), sig.ValidatorAddress.Bytes(), "sigs[%d] address", i)
			require.Equal(t, ts, sig.Timestamp, "sigs[%d] timestamp", i)
			gotSig, ok := sig.Signature.Get()
			require.True(t, ok, "sigs[%d] should carry the ed25519 signature", i)
			require.Equal(t, want[string(genVals[i].PubKey.Bytes())], gotSig.Bytes(), "sigs[%d] signature bytes", i)
		}
	})

	t.Run("strict subset of committee signs", func(t *testing.T) {
		// genVals[0] and genVals[2] sign.
		subset := []atypes.SecretKey{keys[0], keys[2]}
		qc, signed := mkQC(subset)
		want := sigBytesByKey(signed)
		didSign := map[int]bool{0: true, 2: true}
		sigs := commitSigsFromQC(genVals, qc, ts)
		require.Len(t, sigs, len(genVals))
		for i, sig := range sigs {
			if didSign[i] {
				require.Equal(t, types.BlockIDFlagCommit, sig.BlockIDFlag, "sigs[%d]", i)
				require.Equal(t, genVals[i].PubKey.Address().Bytes(), sig.ValidatorAddress.Bytes(), "sigs[%d] address", i)
				require.Equal(t, ts, sig.Timestamp, "sigs[%d] timestamp", i)
				gotSig, ok := sig.Signature.Get()
				require.True(t, ok, "sigs[%d] should carry the ed25519 signature", i)
				require.Equal(t, want[string(genVals[i].PubKey.Bytes())], gotSig.Bytes(), "sigs[%d] signature bytes", i)
			} else {
				assertAbsent(t, sig, fmt.Sprintf("sigs[%d]", i))
			}
		}
	})

	t.Run("None qc (genesis or pruned)", func(t *testing.T) {
		sigs := commitSigsFromQC(genVals, utils.None[*atypes.CommitQC](), ts)
		require.Len(t, sigs, len(genVals), "all-absent slice still sized to validator set")
		for i, sig := range sigs {
			assertAbsent(t, sig, fmt.Sprintf("sigs[%d]", i))
		}
	})

	t.Run("QC signer not in genVals (committee drift)", func(t *testing.T) {
		// QC contains genVals[0], genVals[2], and one extra signer that
		// isn't in genVals at all. The extra is dropped silently; the
		// other two still land in their correct slots.
		extra := atypes.GenSecretKey(rng)
		signers := []atypes.SecretKey{keys[0], keys[2], extra}
		qc, _ := mkQC(signers)
		didSign := map[int]bool{0: true, 2: true}
		sigs := commitSigsFromQC(genVals, qc, ts)
		require.Len(t, sigs, len(genVals))
		for i, sig := range sigs {
			if didSign[i] {
				require.Equal(t, types.BlockIDFlagCommit, sig.BlockIDFlag, "sigs[%d]", i)
				require.Equal(t, genVals[i].PubKey.Address().Bytes(), sig.ValidatorAddress.Bytes(), "sigs[%d] address", i)
			} else {
				assertAbsent(t, sig, fmt.Sprintf("sigs[%d]", i))
			}
		}
	})

	t.Run("empty genVals", func(t *testing.T) {
		// Edge: a chain with no validators (impossible in practice, but
		// the function must not panic — used to share early-exit code
		// with downstream consumers that may iterate len(sigs)).
		qc, _ := mkQC(keys)
		require.Empty(t, commitSigsFromQC(nil, qc, ts))
		require.Empty(t, commitSigsFromQC(nil, utils.None[*atypes.CommitQC](), ts))
	})
}

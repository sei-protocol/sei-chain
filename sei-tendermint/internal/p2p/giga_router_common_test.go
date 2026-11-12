package p2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/giga/apphash"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	gigapb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"
)

func registerEvmProxyForTest(t *testing.T, router *gigaRouterCommon, validator atypes.PublicKey, rpcURL url.URL) *ethrpc.Client {
	t.Helper()
	client, err := ethrpc.DialContext(t.Context(), rpcURL.String())
	require.NoError(t, err)
	t.Cleanup(client.Close)
	for proxies := range router.proxies.Lock() {
		proxies[validator] = client
	}
	return client
}

type fixedHeightApp struct {
	abci.BaseApplication
	height int64
}

func (a *fixedHeightApp) LastBlockHeight() int64 { return a.height }

func (a *fixedHeightApp) Info() *abci.ResponseInfo {
	return &abci.ResponseInfo{LastBlockHeight: a.height}
}

// manualAppHashBuilder is an AppHashBuilder whose app hashes become available
// only when the test adds them, so a committed block's app hash reaches the
// router only when the test says so. With publishOnReport set, ReportBlockHash
// makes the block's app hash available immediately, which is what a router
// under test needs in order to propose the block it just ran.
type manualAppHashBuilder struct {
	state           utils.Watch[*manualAppHashBuilderState]
	publishOnReport bool
}

type manualAppHashBuilderState struct {
	appHashes map[uint64]*apphash.AppHashData
}

func newManualAppHashBuilder() *manualAppHashBuilder {
	return &manualAppHashBuilder{state: utils.NewWatch(&manualAppHashBuilderState{
		appHashes: map[uint64]*apphash.AppHashData{},
	})}
}

func newPublishingAppHashBuilder() *manualAppHashBuilder {
	builder := newManualAppHashBuilder()
	builder.publishOnReport = true
	return builder
}

func (b *manualAppHashBuilder) ReportBlockHash(_ context.Context, blockHeight uint64, blockHash [32]byte) error {
	if b.publishOnReport {
		b.add(apphash.NewAppHashData(
			0, blockHeight, blockHash, [32]byte{}, apphash.BUDHash{}, apphash.ReceiptHash{}, [32]byte{}))
	}
	return nil
}

func (b *manualAppHashBuilder) ReportStateHash(context.Context, uint64, [32]byte, map[string][]byte) error {
	return nil
}

func (b *manualAppHashBuilder) ReportBUDHash(context.Context, uint64, [32]byte) error { return nil }

func (b *manualAppHashBuilder) ReportReceiptHash(context.Context, uint64, apphash.ReceiptHash) error {
	return nil
}

func (b *manualAppHashBuilder) GetAppHash(ctx context.Context, blockHeight uint64) (*apphash.AppHashData, error) {
	var appHash *apphash.AppHashData
	for state, ctrl := range b.state.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return state.appHashes[blockHeight] != nil }); err != nil {
			return nil, err
		}
		appHash = state.appHashes[blockHeight]
	}
	return appHash, nil
}

func (b *manualAppHashBuilder) Bounds() (uint64, uint64, bool, error) {
	var lowest, highest uint64
	var ok bool
	for state := range b.state.Lock() {
		for height := range state.appHashes {
			if !ok || height < lowest {
				lowest = height
			}
			if !ok || height > highest {
				highest = height
			}
			ok = true
		}
	}
	return lowest, highest, ok, nil
}

func (b *manualAppHashBuilder) SetupComplete(context.Context, uint64) error { return nil }

func (b *manualAppHashBuilder) Close() error { return nil }

// add makes appHash available.
func (b *manualAppHashBuilder) add(appHash *apphash.AppHashData) {
	for state, ctrl := range b.state.Lock() {
		state.appHashes[appHash.BlockHeight()] = appHash
		ctrl.Updated()
	}
}

func testAppHashData(height atypes.RoadIndex) *apphash.AppHashData {
	var blockHash [32]byte
	blockHash[0] = byte(height)
	sum := [32]byte{byte(height)}
	return apphash.NewAppHashData(0, uint64(height), blockHash, sum, apphash.BUDHash{}, apphash.ReceiptHash{}, [32]byte{})
}

// newAppHashTestRouter returns a single-validator router over an in-memory
// block store, with a test app, a hash store the caller publishes by hand, and
// an executor of that app over the store.
func newAppHashTestRouter(
	t *testing.T,
	rng utils.Rng,
	key atypes.SecretKey,
) (*testApp, *manualAppHashBuilder, *data.State, *gigaRouterCommon, *execution.Executor) {
	t.Helper()
	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "apphash-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		AppState:        testAppStateJSON(rng),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	app := newTestApp()
	hashBuilder := newManualAppHashBuilder()
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	cfg := &GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     map[atypes.PublicKey]GigaNodeAddr{key.Public(): {}},
		PersistentStateDir: t.TempDir(),
		GenDoc:             genDoc,
		App:                proxy.New(app),
		AppHashBuilder:     hashBuilder,
	}
	state, err := BuildDataState(cfg, db)
	require.NoError(t, err)
	router := &gigaRouterCommon{
		cfg:  cfg,
		data: state,
		app:  cfg.App,
	}
	return app, hashBuilder, state, router, newTestExecutor(cfg, db)
}

// TestExecutionRunsAheadOfAppHashPush verifies that execution does not wait for
// consensus: the executor commits every durable block while no app hash is being
// pushed, and the push then catches up from the app hash builder.
func TestExecutionRunsAheadOfAppHashPush(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	app, hashBuilder, state, router, executor := newAppHashTestRouter(t, rng, key)

	qc, blocks := data.TestCommitQC(rng, state.Anchor().Load().Epochs[0], []atypes.SecretKey{key}, utils.None[*atypes.CommitQC]())
	qc2, blocks2 := data.TestCommitQC(rng, state.Anchor().Load().Epochs[0], []atypes.SecretKey{key}, utils.Some(qc.QC()))
	qc3, blocks3 := data.TestCommitQC(rng, state.Anchor().Load().Epochs[0], []atypes.SecretKey{key}, utils.Some(qc2.QC()))
	require.Equal(t, state.Anchor().Load().Epochs[0].RoadRange().First, qc.Index())
	hashBuilder.publishOnReport = true

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("persist", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushPartialBlock(ctx, qc, data.TestBlockMap(blocks)); err != nil {
			return fmt.Errorf("state.PushPartialBlock(): %w", err)
		}
		if err := state.PushPartialBlock(ctx, qc2, data.TestBlockMap(blocks2)); err != nil {
			return fmt.Errorf("state.PushPartialBlock(): %w", err)
		}
		if err := state.PushPartialBlock(ctx, qc3, data.TestBlockMap(blocks3)); err != nil {
			return fmt.Errorf("state.PushPartialBlock(): %w", err)
		}
		s.SpawnBgNamed("execute", func() error { return utils.IgnoreCancel(executor.Run(ctx)) })
		if _, err := app.WaitForBlocks(ctx, 3); err != nil {
			return err
		}
		s.SpawnBgNamed("pushAppHashes", func() error {
			return utils.IgnoreCancel(router.runPushAppHashes(ctx))
		})
		if _, err := state.AppVote(ctx, qc3.Index()); err != nil {
			return fmt.Errorf("AppVote(%v): %w", qc3.Index(), err)
		}
		return nil
	}))
}

// TestPushAppHashesResumesAtNextAppProposal pins the restart path: the app
// hashes of blocks executed but not yet pushed come from the app hash builder,
// with no executor running.
func TestPushAppHashesResumesAtNextAppProposal(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	_, hashBuilder, state, router, _ := newAppHashTestRouter(t, rng, key)

	qc, blocks := data.TestCommitQC(rng, state.Anchor().Load().Epochs[0], []atypes.SecretKey{key}, utils.None[*atypes.CommitQC]())
	qc2, blocks2 := data.TestCommitQC(rng, state.Anchor().Load().Epochs[0], []atypes.SecretKey{key}, utils.Some(qc.QC()))
	last := qc2.Index()
	require.Equal(t, state.Anchor().Load().Epochs[0].RoadRange().First, qc.Index())

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("persist", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushPartialBlock(ctx, qc, data.TestBlockMap(blocks)); err != nil {
			return fmt.Errorf("state.PushPartialBlock(): %w", err)
		}
		if err := state.PushPartialBlock(ctx, qc2, data.TestBlockMap(blocks2)); err != nil {
			return fmt.Errorf("state.PushPartialBlock(): %w", err)
		}
		for n := state.NextAppProposal(); n <= last; n++ {
			hashBuilder.add(testAppHashData(n))
		}
		s.SpawnBgNamed("pushAppHashes", func() error {
			return utils.IgnoreCancel(router.runPushAppHashes(ctx))
		})
		if _, err := state.AppVote(ctx, last); err != nil {
			return fmt.Errorf("AppVote(%v): %w", last, err)
		}
		return nil
	}))
	require.Equal(t, last+1, state.NextAppProposal())
}

func TestCommitteeWeights(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	wantPK := utils.OrPanic1(atypes.PublicKeyFromBytes(sk.Public().Bytes()))
	got, err := committeeWeights([]abci.ValidatorUpdate{{
		PubKey: crypto.PubKeyToProto(sk.Public()),
		Power:  42,
	}})
	require.NoError(t, err)
	require.Equal(t, map[atypes.PublicKey]uint64{wantPK: 42}, got)
}

func TestCommitteeWeights_SkipsZeroPower(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	got, err := committeeWeights([]abci.ValidatorUpdate{{
		PubKey: crypto.PubKeyToProto(sk.Public()),
		Power:  0,
	}})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCommitteeWeights_DuplicateKey(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	pk := crypto.PubKeyToProto(sk.Public())
	_, err := committeeWeights([]abci.ValidatorUpdate{
		{PubKey: pk, Power: 1},
		{PubKey: pk, Power: 2},
	})
	require.Error(t, err)
}

func TestBuildDataStateStartsRecoveryAtAppTip(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	keys := []atypes.SecretKey{key}
	validatorAddrs := map[atypes.PublicKey]GigaNodeAddr{key.Public(): {}}

	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "restart-tip-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	committee, err := atypes.NewCommittee(map[atypes.PublicKey]uint64{key.Public(): 1})
	require.NoError(t, err)
	registry, err := epoch.NewRegistry(committee, atypes.RoadIndex(genDoc.InitialHeight), genDoc.GenesisTime, utils.None[string]())
	require.NoError(t, err)
	ep0 := registry.MustEpoch(0)
	qc, blocks := data.TestCommitQC(rng, ep0, keys, utils.None[*atypes.CommitQC]())
	qc2, blocks2 := data.TestCommitQC(rng, ep0, keys, utils.Some(qc.QC()))
	qc3, blocks3 := data.TestCommitQC(rng, ep0, keys, utils.Some(qc2.QC()))
	last := qc2.Index()

	db := blockstore.NewTestStore(t)
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc, blocks)))
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc2, blocks2)))
	require.NoError(t, db.WriteBlock(types.NewRoadBlock(qc3, blocks3)))
	require.NoError(t, db.Flush())
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	cfg := &GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     validatorAddrs,
		PersistentStateDir: t.TempDir(),
		GenDoc:             genDoc,
		App:                proxy.New(&fixedHeightApp{height: int64(last)}),
		AppHashBuilder:     newPublishingAppHashBuilder(),
	}
	state, err := BuildDataState(cfg, db)
	require.NoError(t, err)
	got, err := state.TryBlock(atypes.LaneBlockPos{Index: last})
	require.NoError(t, err)
	require.Equal(t, blocks2[0].Header().Hash(), got.Header().Hash())
}

func TestGigaRouterCommon_ValidatorsAtGlobalHeight(t *testing.T) {
	rng := utils.TestRng()
	low := atypes.GenSecretKey(rng)
	mid := atypes.GenSecretKey(rng)
	high := atypes.GenSecretKey(rng)
	keys := []atypes.SecretKey{low, mid, high}
	router, registry := testGigaRouterWithData(t, map[atypes.PublicKey]GigaNodeAddr{
		low.Public():  {},
		mid.Public():  {},
		high.Public(): {},
	})
	first := router.data.Anchor().Load().Epochs[0].RoadRange().First

	got, err := router.Validators(first)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, []int64{1, 1, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})

	_, err = router.Validators(0)
	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)

	_, err = router.Validators(first + 100)
	require.ErrorIs(t, err, coretypes.ErrHeightExceedsChainHead)

	weights := map[atypes.PublicKey]uint64{
		low.Public():  1,
		mid.Public():  5,
		high.Public(): 10,
	}
	require.NoError(t, registry.TestPushEpoch(0, weights))
	ep2 := registry.MustEpoch(2)
	got, err = router.Validators(first)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 1, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})

	require.NoError(t, registry.TestPushEpoch(1, weights))
	n := ep2.RoadRange().First
	router = testRouterAtRoad(t, registry, keys, ep2, n)
	got, err = router.Validators(n)
	require.NoError(t, err)
	require.Equal(t, []int64{10, 5, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})
	require.Equal(t, high.Public().Bytes(), got[0].PubKey.Bytes())
	require.Equal(t, mid.Public().Bytes(), got[1].PubKey.Bytes())
	require.Equal(t, low.Public().Bytes(), got[2].PubKey.Bytes())

	require.NoError(t, registry.PruneBefore(2))
	got, err = router.Validators(n)
	require.NoError(t, err)
	require.Equal(t, []int64{10, 5, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})
	_, err = router.Validators(n + 1)
	require.ErrorIs(t, err, coretypes.ErrHeightExceedsChainHead)

	require.NoError(t, registry.TestPushEpoch(2, weights))
	require.NoError(t, registry.TestPushEpoch(3, weights))
	ep4 := registry.MustEpoch(4)
	router = testRouterAtRoad(t, registry, keys, ep4, ep4.RoadRange().First)
	require.NoError(t, registry.PruneBefore(4))
	_, err = router.Validators(n)
	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)
}

func testRouterAtRoad(t *testing.T, registry *epoch.Registry, keys []atypes.SecretKey, ep *atypes.Epoch, road atypes.RoadIndex) *gigaRouterCommon {
	t.Helper()
	proposal, blocks := atypes.ProposalAtBlocks(ep, atypes.View{Index: road, Number: 0}, 1)
	votes := make([]*atypes.Signed[*atypes.CommitVote], 0, len(keys))
	prepareQC := atypes.BuildPrepareQC(keys, proposal)
	for _, k := range keys {
		votes = append(votes, atypes.Sign(k, atypes.NewCommitVote(prepareQC)))
	}
	headers := make([]*atypes.BlockHeader, len(blocks))
	for i, b := range blocks {
		headers[i] = b.Header()
	}
	qc := atypes.NewFullCommitQC(atypes.NewCommitQC(votes), headers)
	store := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.WriteBlock(types.NewRoadBlock(qc, blocks)))
	require.NoError(t, store.Flush())
	state, err := data.NewState(&data.Config{Registry: registry}, store)
	require.NoError(t, err)
	return &gigaRouterCommon{data: state}
}

func testGigaRouterWithData(t *testing.T, addrs map[atypes.PublicKey]GigaNodeAddr) (*gigaRouterCommon, *epoch.Registry) {
	t.Helper()
	weights := make(map[atypes.PublicKey]uint64, len(addrs))
	for key := range addrs {
		weights[key] = 1
	}
	committee, err := atypes.NewCommittee(weights)
	require.NoError(t, err)
	state, registry := testDataState(t, committee)
	return &gigaRouterCommon{data: state}, registry
}

func testDataState(t *testing.T, committee *atypes.Committee) (*data.State, *epoch.Registry) {
	t.Helper()
	registry, err := epoch.NewRegistry(committee, 1, time.Now(), utils.None[string]())
	require.NoError(t, err)
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	state, err := data.NewState(&data.Config{Registry: registry}, db)
	require.NoError(t, err)
	return state, registry
}

func testDataWithAnchor(t *testing.T, anchor data.Anchor) *data.State {
	t.Helper()
	state, _ := testDataState(t, anchor.Epochs[0].Committee())
	data.TestSetAnchor(state, anchor)
	return state
}

func testEpoch(index atypes.EpochIndex, weights map[atypes.PublicKey]uint64) *atypes.Epoch {
	committee := utils.OrPanic1(atypes.NewCommittee(weights))
	return atypes.NewEpoch(index, atypes.RoadRange{First: atypes.RoadIndex(index), Next: atypes.RoadIndex(index) + 1}, committee)
}

func testAnchor(ep *atypes.Epoch) data.Anchor { return data.TestAnchor(ep) }

func testCertifiedAnchor(ep, next *atypes.Epoch, keys []atypes.SecretKey) data.Anchor {
	qc := atypes.BuildCommitQC(ep, keys, utils.None[*atypes.CommitQC](), nil)
	return data.Anchor{
		First: utils.Some(data.AnchorFirst{
			CommitQC: qc,
			AppQC:    data.TestAppQC(keys, atypes.NewAppProposal(qc.View(), nil)),
		}),
		Epochs: [2]*atypes.Epoch{ep, next},
	}
}

// settledEpochs publishes anchors with the supplied first commit epoch.
type settledEpochs struct {
	state *data.State
}

func newSettledEpochs(t *testing.T, ep *atypes.Epoch) *settledEpochs {
	t.Helper()
	return &settledEpochs{state: testDataWithAnchor(t, testAnchor(ep))}
}

func (e *settledEpochs) store(ep *atypes.Epoch) {
	data.TestSetAnchor(e.state, testAnchor(ep))
}

func TestGigaRouterCommon_ValidatorAddrPrefersConfiguredBook(t *testing.T) {
	rng := utils.TestRng()
	validator := atypes.GenSecretKey(rng).Public()
	configured := GigaNodeAddr{
		Key:      makeKey(rng).Public(),
		HostPort: tcp.HostPort{Hostname: "configured.example", Port: 26656},
		EVMRPC:   *utils.OrPanic1(url.Parse("http://configured.example:8545")),
	}
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{validator: configured}},
		liveAddrs: utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{validator: {
			Key:      makeKey(rng).Public(),
			HostPort: tcp.HostPort{Hostname: "advertised.example", Port: 26656},
			EVMRPC:   *utils.OrPanic1(url.Parse("http://advertised.example:8545")),
		}}),
	}
	got, ok := router.validatorAddr(validator)
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(configured, got))
}

func TestGigaRouterCommon_AcceptInboundRefusesUnroutableEvmRPC(t *testing.T) {
	rng := utils.TestRng()
	validatorKey := atypes.GenSecretKey(rng)
	peerKey := makeKey(rng)
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{validatorKey.Public(): 1}))
	router := &gigaRouterCommon{
		cfg:             &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{}},
		data:            epochs.state,
		liveAddrs:       utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{}),
		liveAddrVersion: utils.NewAtomicSend(uint64(0)),
	}
	for _, evmRPC := range []string{
		"http://127.0.0.1:8545",
		"http://localhost:8545",
		"http://169.254.169.254/",
		"http://0.0.0.0:8545",
		"http://[::]:8545",
	} {
		// The claim proves committee identity, so the peer is still served as a
		// validator; only its advertised address is refused.
		accepted := router.acceptInbound(&handshakedConn{msg: &handshakeMsg{
			NodeAuth: NodeChallengeSig{key: peerKey.Public()},
			handshakeSpec: handshakeSpec{
				SelfAddr: utils.Some(NodeAddress{
					NodeID:   peerKey.Public().NodeID(),
					Hostname: "validator.example",
					Port:     26656,
				}),
				SeiGigaConnection: true,
			},
			GigaClaim: utils.Some(gigaHandshakeClaim{Validator: validatorKey.Public(), evmRPC: evmRPC}),
		}})
		require.True(t, accepted.IsPresent())
		_, ok := router.validatorAddr(validatorKey.Public())
		require.False(t, ok)
	}
}

func TestGigaRouterCommon_RunInboundConnLearnsMemberAndClosesOnLeave(t *testing.T) {
	rng := utils.TestRng()
	validatorKey := atypes.GenSecretKey(rng)
	localKey := makeKey(rng)
	peerKey := makeKey(rng)
	selfAddr := NodeAddress{NodeID: peerKey.Public().NodeID(), Hostname: "validator.example", Port: 26656}
	evmRPC := *utils.OrPanic1(url.Parse("ws://validator.example:8546"))
	dummy := atypes.GenSecretKey(rng)
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{validatorKey.Public(): 1}))
	genesis := map[atypes.PublicKey]GigaNodeAddr{dummy.Public(): {Key: makeKey(rng).Public()}}
	router, _ := testGigaRouterWithData(t, genesis)
	router.data = epochs.state
	router.liveAddrs = utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{})
	router.liveAddrVersion = utils.NewAtomicSend(uint64(0))
	router.poolIn = giga.NewPool[NodePublicKey, rpc.Server[giga.API]]()
	router.poolInCommittee = giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]]()
	router.inboundFullnodeCount = utils.NewMutex(new(int64))
	router.inboundFullnodeCap = 10
	router.service = giga.NewFullNodeService(router.data)
	router.key = localKey
	router.cfg = &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{}}
	gigaAddr := NodeAddress{NodeID: localKey.Public().NodeID(), Hostname: "giga.example", Port: 26656}
	router.selfAddr = utils.Some(gigaAddr)
	localSpec := handshakeSpec{SelfAddr: router.selfAddr, SeiGigaConnection: true}
	want := GigaNodeAddr{
		Key:      peerKey.Public(),
		HostPort: tcp.HostPort{Hostname: selfAddr.Hostname, Port: selfAddr.Port},
		EVMRPC:   evmRPC,
	}
	joinerKey := makeKey(rng)
	require.False(t, router.acceptInbound(&handshakedConn{msg: &handshakeMsg{
		NodeAuth: NodeChallengeSig{key: joinerKey.Public()},
		handshakeSpec: handshakeSpec{
			SelfAddr: utils.Some(NodeAddress{
				NodeID:   joinerKey.Public().NodeID(),
				Hostname: "joiner.example",
				Port:     26656,
			}),
			SeiGigaConnection: true,
		},
		GigaClaim: utils.Some(gigaHandshakeClaim{
			Validator: dummy.Public(),
			evmRPC:    "http://joiner.example:8545",
		}),
	}}).IsPresent())
	_, ok := router.validatorAddr(dummy.Public())
	require.False(t, ok)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		pair, err := handshakePair(ctx, s,
			[2]NodeSecretKey{localKey, peerKey},
			[2]handshakeSpec{
				localSpec,
				{SelfAddr: utils.Some(selfAddr), SeiGigaConnection: true},
			},
			[2]utils.Option[handshakeOffer]{
				router.offer,
				utils.Some(handshakeOffer{ValidatorKey: validatorKey, EVMRPC: evmRPC}),
			},
		)
		if err != nil {
			return err
		}
		if err := utils.TestDiff(gigaAddr, pair[1].msg.SelfAddr.OrPanic("missing SelfAddr")); err != nil {
			return err
		}
		addrUpdates := router.liveAddrVersion.Subscribe()
		finished := scope.Spawn1(s, func() (struct{}, error) {
			err := router.RunInboundConn(ctx, pair[0])
			if errors.Is(err, errGigaMembershipChanged) {
				return struct{}{}, nil
			}
			if err != nil {
				return struct{}{}, err
			}
			return struct{}{}, fmt.Errorf("RunInboundConn() = nil, want %v", errGigaMembershipChanged)
		})
		if _, err := addrUpdates.Wait(ctx, func(v uint64) bool { return v > 0 }); err != nil {
			return err
		}
		got, ok := router.validatorAddr(validatorKey.Public())
		if !ok {
			return fmt.Errorf("validatorAddr(%v) missing after inbound", validatorKey.Public())
		}
		if err := utils.TestDiff(want, got); err != nil {
			return err
		}
		epochs.store(testEpoch(2, map[atypes.PublicKey]uint64{dummy.Public(): 1}))
		_, err = finished.Join(ctx)
		if err != nil {
			return err
		}
		if err := router.stopStaleSessions(
			ctx,
			map[atypes.PublicKey]*memberSession{},
			router.data.Anchor().Load(),
			router.data.Anchor().Load().NextIndexEpoch(),
		); err != nil {
			return err
		}
		if _, ok := router.validatorAddr(validatorKey.Public()); ok {
			return fmt.Errorf("validatorAddr(%v) kept after leave", validatorKey.Public())
		}
		return nil
	}))
}

func TestGigaRouterCommon_RunPerCommitteeMemberFollowsCommittee(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	b := atypes.GenSecretKey(rng).Public()
	epochs := newSettledEpochs(t, testEpoch(2, map[atypes.PublicKey]uint64{b: 1}))
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
		}},

		data:            epochs.state,
		liveAddrs:       utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{}),
		liveAddrVersion: utils.NewAtomicSend(uint64(0)),
	}

	startedA := make(chan struct{}, 1)
	startedB := make(chan struct{}, 1)
	stoppedB := make(chan struct{}, 1)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, func(ctx context.Context, validator atypes.PublicKey, _ GigaNodeAddr) error {
				switch {
				case validator.Compare(a) == 0:
					startedA <- struct{}{}
				case validator.Compare(b) == 0:
					startedB <- struct{}{}
				}
				<-ctx.Done()
				if validator.Compare(b) == 0 {
					stoppedB <- struct{}{}
				}
				return ctx.Err()
			}))
		})
		<-startedB
		select {
		case <-startedA:
			return fmt.Errorf("a started while outside the committee")
		default:
		}
		epochs.store(testEpoch(3, map[atypes.PublicKey]uint64{a: 1, b: 1}))
		<-startedA
		epochs.store(testEpoch(4, map[atypes.PublicKey]uint64{a: 1}))
		<-stoppedB
		select {
		case <-startedA:
			return fmt.Errorf("a restarted while still a member")
		default:
		}
		return nil
	})
	require.NoError(t, err)
}

func TestGigaRouterCommon_RunPerCommitteeMemberDialsBothCommitteesUntilStable(t *testing.T) {
	rng := utils.TestRng()
	keys := []atypes.SecretKey{atypes.GenSecretKey(rng), atypes.GenSecretKey(rng), atypes.GenSecretKey(rng)}
	a, b, c := keys[0].Public(), keys[1].Public(), keys[2].Public()
	d := atypes.GenSecretKey(rng).Public()
	ep1 := testEpoch(1, map[atypes.PublicKey]uint64{a: 1, b: 1})
	ep2 := testEpoch(2, map[atypes.PublicKey]uint64{a: 1, b: 1, c: 1})
	ep3 := testEpoch(3, map[atypes.PublicKey]uint64{a: 1, c: 1, d: 1})
	state := testDataWithAnchor(t, testCertifiedAnchor(ep1, ep2, keys[:2]))
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
			c: {Key: makeKey(rng).Public()},
			d: {Key: makeKey(rng).Public()},
		}},
		data:            state,
		liveAddrs:       utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{}),
		liveAddrVersion: utils.NewAtomicSend(uint64(0)),
	}

	started := make(chan atypes.PublicKey, 4)
	stoppedB := make(chan struct{}, 1)
	onStart := map[atypes.PublicKey]struct{}{}
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, func(ctx context.Context, validator atypes.PublicKey, _ GigaNodeAddr) error {
				started <- validator
				<-ctx.Done()
				if validator.Compare(b) == 0 {
					stoppedB <- struct{}{}
				}
				return ctx.Err()
			}))
		})
		for range 3 {
			onStart[<-started] = struct{}{}
		}
		data.TestSetAnchor(state, testCertifiedAnchor(ep2, ep3, keys))
		<-started // d joined, so the anchor update already ran
		select {
		case <-stoppedB:
			return fmt.Errorf("b stopped while Anchor retained its membership epoch")
		default:
		}
		data.TestSetAnchor(state, testAnchor(ep3))
		<-stoppedB
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, map[atypes.PublicKey]struct{}{a: {}, b: {}, c: {}}, onStart)
}

func TestGigaRouterCommon_RunPerCommitteeMemberRunsOneSessionPerMember(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	// b has no address-book entry, so an epoch containing only b starts nothing.
	b := atypes.GenSecretKey(rng).Public()
	epochs := newSettledEpochs(t, testEpoch(2, map[atypes.PublicKey]uint64{a: 1}))
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
		}},

		data:            epochs.state,
		liveAddrs:       utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{}),
		liveAddrVersion: utils.NewAtomicSend(uint64(0)),
	}

	started := make(chan struct{}, 2)
	canceled := make(chan struct{}, 2)
	release := make(chan struct{})
	var live atomic.Int64
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, func(ctx context.Context, _ atypes.PublicKey, _ GigaNodeAddr) error {
				started <- struct{}{}
				if n := live.Add(1); n > 1 {
					return fmt.Errorf("%d concurrent sessions for the same member", n)
				}
				<-ctx.Done()
				canceled <- struct{}{}
				// Keep the first session live after cancellation.
				<-release
				live.Add(-1)
				return ctx.Err()
			}))
		})
		<-started
		epochs.store(testEpoch(3, map[atypes.PublicKey]uint64{b: 1}))
		<-canceled
		epochs.store(testEpoch(4, map[atypes.PublicKey]uint64{a: 1}))
		close(release)
		<-started
		return nil
	})
	require.NoError(t, err)
}

func TestGigaRouterCommon_RunPerCommitteeMemberCancelsAllDepartingBeforeAwait(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	b := atypes.GenSecretKey(rng).Public()
	c := atypes.GenSecretKey(rng).Public()
	epochs := newSettledEpochs(t, testEpoch(2, map[atypes.PublicKey]uint64{a: 1, b: 1}))
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
		}},

		data:            epochs.state,
		liveAddrs:       utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{}),
		liveAddrVersion: utils.NewAtomicSend(uint64(0)),
	}

	started := make(chan atypes.PublicKey, 2)
	canceled := make(chan struct{}, 2)
	release := make(chan struct{})
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, func(ctx context.Context, validator atypes.PublicKey, _ GigaNodeAddr) error {
				started <- validator
				<-ctx.Done()
				canceled <- struct{}{}
				<-release
				return ctx.Err()
			}))
		})
		for range 2 {
			<-started
		}
		epochs.store(testEpoch(3, map[atypes.PublicKey]uint64{c: 1}))
		for range 2 {
			<-canceled
		}
		close(release)
		return nil
	})
	require.NoError(t, err)
}

func TestGigaRouterCommon_CommitteeTasksReturnWhenWorkReturns(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	state := testDataWithAnchor(t, testAnchor(testEpoch(2, map[atypes.PublicKey]uint64{a: 1})))
	router := &gigaRouterCommon{data: state}

	changed, err := router.runUntilMembershipChange(t.Context(), a, func(context.Context) error {
		return nil
	})
	require.NoError(t, err)
	require.False(t, changed)
}

func TestGigaRouterCommon_RunUntilMembershipChangeCancelsFWhenMembershipChanges(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	b := atypes.GenSecretKey(rng).Public()
	state := testDataWithAnchor(t, testAnchor(testEpoch(2, map[atypes.PublicKey]uint64{a: 1, b: 1})))
	router := &gigaRouterCommon{data: state}
	started := make(chan struct{})
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			changed, err := router.runUntilMembershipChange(ctx, a, func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			})
			if err != nil {
				return err
			}
			if !changed {
				return fmt.Errorf("membership change must cancel f and report true")
			}
			return nil
		})
		<-started
		data.TestSetAnchor(state, testAnchor(testEpoch(3, map[atypes.PublicKey]uint64{b: 1})))
		return nil
	})
	require.NoError(t, err)
}

// A peer that accepts and then says nothing must return from the handshake.
func TestGigaRouterCommon_OutboundHandshakeBoundedByTimeout(t *testing.T) {
	rng := utils.TestRng()
	addr := tcp.TestReserveAddr()
	listener, err := tcp.Listen(addr)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{HandshakeTimeout: utils.Some(100 * time.Millisecond)},
		key: makeKey(rng),
	}

	err = scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			// Hold the accepted connection open without ever speaking.
			conn, err := listener.AcceptOrClose(ctx)
			if err != nil {
				return err
			}
			return conn.Run(ctx)
		})
		return router.dialAndRunConn(ctx, atypes.GenSecretKey(rng).Public(), makeKey(rng).Public(),
			tcp.HostPort{Hostname: addr.Addr().String(), Port: addr.Port()},
			func(context.Context, rpc.Client[giga.API]) error { return nil })
	})
	// Cancelling the handshake half-closes the socket, so the connection pump
	// often reports EOF before handshake returns the deadline.
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, io.EOF) {
		t.Fatalf("dialAndRunConn() = %v, want context.DeadlineExceeded or io.EOF", err)
	}
}

func TestGigaRouter_InboundFullnodeCap(t *testing.T) {
	rng := utils.TestRng()
	localKey := makeKey(rng)
	persistentKeys := []NodeSecretKey{makeKey(rng), makeKey(rng)}
	validator := atypes.GenSecretKey(rng)
	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "persistent-peer-cap",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	db := blockstore.NewTestStore(t)
	cfg := &GigaRouterCommonConfig{
		DialInterval: time.Second,
		ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			validator.Public(): {Key: makeKey(rng).Public()},
		},
		PersistentStateDir:      t.TempDir(),
		App:                     proxy.New(&fixedHeightApp{height: 1}),
		AppHashBuilder:          newPublishingAppHashBuilder(),
		GenDoc:                  genDoc,
		MaxInboundFullnodePeers: 3,
		PersistentPeerIDs: map[tmtypes.NodeID]struct{}{
			persistentKeys[0].Public().NodeID(): {},
			persistentKeys[1].Public().NodeID(): {},
		},
	}
	state, err := BuildDataState(cfg, db)
	require.NoError(t, err)
	router, err := NewGigaFullnodeRouter(cfg, localKey, state)
	require.NoError(t, err)

	endpoint := Endpoint{AddrPort: tcp.TestReserveAddr()}
	nodeInfo := makeInfo(localKey)
	nodeInfo.ListenAddr = endpoint.AddrPort.String()
	nodeInfo.Network = genDoc.ChainID
	p2pRouter, err := NewRouter(localKey, func() *tmtypes.NodeInfo { return &nodeInfo }, dbm.NewMemDB(), &RouterOptions{
		SelfAddress:              utils.Some(endpoint.NodeAddress(localKey.Public().NodeID())),
		Endpoint:                 endpoint,
		Connection:               conn.DefaultMConnConfig(),
		IncomingConnectionWindow: utils.Some(time.Duration(0)),
		MaxAcceptRate:            rate.Inf,
		MaxDialRate:              rate.Inf,
		Giga:                     utils.Some[GigaRouter](router),
	})
	require.NoError(t, err)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(p2pRouter.Run(ctx)) })
		s.SpawnBg(func() error { return utils.IgnoreCancel(router.Run(ctx)) })
		if err := p2pRouter.WaitForStart(ctx); err != nil {
			return err
		}
		connect := func(peer NodeSecretKey) error {
			// A refused peer completes the handshake and then closes the socket
			// without opening a ping stream. Ping.Call waits until this context
			// ends, so a dead mux has to cancel it. Admitted peers keep connCtx
			// alive and therefore keep their cap slot.
			connCtx, cancel := context.WithCancel(ctx)
			tcpConn, err := tcp.Dial(ctx, endpoint.AddrPort)
			if err != nil {
				cancel()
				return err
			}
			// A failed socket read blocks until its context ends. Cancel here so
			// the mux and Ping.Call observe the refused peer's closed socket.
			s.SpawnBg(func() error {
				_ = tcpConn.Run(connCtx)
				cancel()
				return nil
			})
			hConn, err := handshake(connCtx, tcpConn, peer, handshakeSpec{SeiGigaConnection: true}, utils.None[handshakeOffer]())
			if err != nil {
				cancel()
				return err
			}
			client := rpc.NewClient[giga.API]()
			s.SpawnBg(func() error {
				_ = client.Run(connCtx, hConn.conn)
				cancel()
				return nil
			})
			stream, err := giga.Ping.Call(connCtx, client)
			if err != nil {
				cancel()
				return err
			}
			defer stream.Close()
			if err := stream.Send(connCtx, &gigapb.PingReq{}); err != nil {
				cancel()
				return err
			}
			if _, err := stream.Recv(connCtx); err != nil {
				cancel()
				return err
			}
			return nil
		}

		for range cfg.MaxInboundFullnodePeers {
			if err := connect(makeKey(rng)); err != nil {
				return err
			}
		}
		if err := connect(makeKey(rng)); err == nil {
			return fmt.Errorf("excess peer ping succeeded")
		}
		for _, peer := range persistentKeys {
			if err := connect(peer); err != nil {
				return err
			}
		}
		return nil
	}))
}

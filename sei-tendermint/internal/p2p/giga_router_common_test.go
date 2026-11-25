package p2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	gigapb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func registerEvmProxyForTest(t *testing.T, router *GigaRouterCommon, validator atypes.PublicKey, rpcURL url.URL) *ethrpc.Client {
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
	abevm.Application
	height int64
}

func (a *fixedHeightApp) LastBlockHeight() int64 { return a.height }

func (a *fixedHeightApp) Info() *abevm.ResponseInfo {
	return &abevm.ResponseInfo{LastBlockHeight: a.height}
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

func (b *manualAppHashBuilder) Flush(context.Context) error { return nil }

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

func testAppHashData(height atypes.RoadHeight) *apphash.AppHashData {
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
) (*testApp, *manualAppHashBuilder, *data.State, *GigaRouterCommon, *execution.Executor) {
	t.Helper()
	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "apphash-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		AppState:        testAppStateJSON(rng),
		ConsensusParams: testGigaConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	app := newTestApp(genDoc.AppState)
	hashBuilder := newManualAppHashBuilder()
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	cfg := &GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     map[atypes.PublicKey]GigaNodeAddr{key.Public(): {}},
		PersistentStateDir: t.TempDir(),
		GenDoc:             genDoc,
		App:                abevm.NewProxy(app),
		AppHashBuilder:     hashBuilder,
	}
	registry := testGenesisEpochs(t, genDoc, cfg.ValidatorAddrs)
	state, err := BuildDataState(cfg, db, abevm.TestEpochApp(registry), genDoc.GenesisTime)
	require.NoError(t, err)
	signer := newTestSigner(cfg.App.EvmChainID(), 0)
	router := &GigaRouterCommon{
		cfg:            cfg,
		data:           state,
		validatorAddrs: testValidatorAddrs(state),
		signer:         signer,
	}
	return app, hashBuilder, state, router, newTestExecutor(cfg, db, signer)
}

// testGenesisEpochs returns a registry whose genesis epochs start at genDoc's initial height with
// the default epoch length and are committed to the validators in addrs with unit weight.
func testGenesisEpochs(t *testing.T, genDoc *tmtypes.GenesisDoc, addrs map[atypes.PublicKey]GigaNodeAddr) *epoch.TestRegistry {
	t.Helper()
	weights := make(map[atypes.PublicKey]uint64, len(addrs))
	for key := range addrs {
		weights[key] = 1
	}
	committee, err := atypes.NewCommittee(weights)
	require.NoError(t, err)
	registry, err := epoch.TestGenesisRegistry(committee, atypes.RoadHeight(genDoc.InitialHeight), epoch.DefaultEpochLength)
	require.NoError(t, err)
	return registry
}

// TestExecutionRunsAheadOfAppHashPush verifies that execution does not wait for
// consensus: the executor commits every durable block while no app hash is being
// pushed, and the push then catches up from the app hash builder.
func TestExecutionRunsAheadOfAppHashPush(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	app, hashBuilder, state, router, executor := newAppHashTestRouter(t, rng, key)

	qc, blocks := data.TestCommitQCWithPayload(rng, state.Anchor().Load().Epochs[0], []atypes.SecretKey{key}, utils.None[*atypes.CommitQC](), func() *atypes.Payload { return atypes.GenEthPayload(rng) })
	qc2, blocks2 := data.TestCommitQCWithPayload(rng, state.Anchor().Load().Epochs[0], []atypes.SecretKey{key}, utils.Some(qc.QC()), func() *atypes.Payload { return atypes.GenEthPayload(rng) })
	qc3, blocks3 := data.TestCommitQCWithPayload(rng, state.Anchor().Load().Epochs[0], []atypes.SecretKey{key}, utils.Some(qc2.QC()), func() *atypes.Payload { return atypes.GenEthPayload(rng) })
	require.Equal(t, state.Anchor().Load().Epochs[0].RoadRange().First, qc.Height())
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
		if _, err := state.AppVote(ctx, qc3.Height()); err != nil {
			return fmt.Errorf("AppVote(%v): %w", qc3.Height(), err)
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
	last := qc2.Height()
	require.Equal(t, state.Anchor().Load().Epochs[0].RoadRange().First, qc.Height())

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

func TestBuildDataStateStartsRecoveryAtAppTip(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	keys := []atypes.SecretKey{key}
	validatorAddrs := map[atypes.PublicKey]GigaNodeAddr{key.Public(): {}}

	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "restart-tip-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: testGigaConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	committee, err := atypes.NewCommittee(map[atypes.PublicKey]uint64{key.Public(): 1})
	require.NoError(t, err)
	registry, err := epoch.TestGenesisRegistry(committee, atypes.RoadHeight(genDoc.InitialHeight), epoch.DefaultEpochLength)
	require.NoError(t, err)
	ep0 := registry.MustEpoch(0)
	qc, blocks := data.TestCommitQC(rng, ep0, keys, utils.None[*atypes.CommitQC]())
	qc2, blocks2 := data.TestCommitQC(rng, ep0, keys, utils.Some(qc.QC()))
	qc3, blocks3 := data.TestCommitQC(rng, ep0, keys, utils.Some(qc2.QC()))
	last := qc2.Height()

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
		App:                abevm.NewProxy(&fixedHeightApp{height: int64(last)}),
		AppHashBuilder:     newPublishingAppHashBuilder(),
	}
	state, err := BuildDataState(cfg, db, abevm.TestEpochApp(registry), genDoc.GenesisTime)
	require.NoError(t, err)
	got, err := state.TryBlock(atypes.LaneBlockPos{Height: last})
	require.NoError(t, err)
	require.Equal(t, blocks2[0].Header().Hash(), got.Header().Hash())
}

func TestGigaRouterCommon_EvmBlockParentHash(t *testing.T) {
	rng := utils.TestRng()
	router := &GigaRouterCommon{cfg: &GigaRouterCommonConfig{GenDoc: &tmtypes.GenesisDoc{ChainID: "test"}}}
	block := atypes.GenSuperBlock(rng)

	block.ParentHash = utils.None[atypes.RoadBlockHeaderHash]()
	require.Equal(t, common.Hash{}, router.evmBlock(block).ParentHash)

	parent := atypes.GenRoadBlockHeaderHash(rng)
	block.ParentHash = utils.Some(parent)
	got := router.evmBlock(block).ParentHash
	require.Equal(t, common.Hash(parent), got)
}

// The block RPC of a later road block names the previous road block as its parent, and that of the
// first road block names none.
func TestGigaRouterCommon_BlockByNumberLinksConsecutiveRoadBlocks(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := atypes.GenCommittee(rng, 3)
	state, registry := testDataState(t, committee)
	ep := registry.MustEpoch(0)
	first, firstBlocks := data.TestCommitQCOnLane(ep, keys, utils.None[*atypes.CommitQC](), ep.Committee().Lanes().At(0))
	second, secondBlocks := data.TestCommitQCOnLane(ep, keys, utils.Some(first.QC()), ep.Committee().Lanes().At(1))
	require.NoError(t, state.PushPartialBlock(t.Context(), first, data.TestBlockMap(firstBlocks)))
	require.NoError(t, state.PushPartialBlock(t.Context(), second, data.TestBlockMap(secondBlocks)))
	router := &GigaRouterCommon{data: state, cfg: &GigaRouterCommonConfig{GenDoc: &tmtypes.GenesisDoc{ChainID: "test"}}}

	gotFirst, err := router.BlockByNumber(t.Context(), first.Height())
	require.NoError(t, err)
	gotSecond, err := router.BlockByNumber(t.Context(), second.Height())
	require.NoError(t, err)
	require.Equal(t, common.Hash{}, gotFirst.ParentHash)
	require.Equal(t, gotFirst.Hash, gotSecond.ParentHash)
}

func testDataState(t *testing.T, committee *atypes.Committee) (*data.State, *epoch.TestRegistry) {
	t.Helper()
	registry, err := epoch.TestGenesisRegistry(committee, 1, epoch.DefaultEpochLength)
	require.NoError(t, err)
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	state, err := data.NewState(&data.Config{App: abevm.TestEpochApp(registry)}, db)
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
	return atypes.NewEpoch(index, atypes.RoadRange{First: atypes.RoadHeight(index), Next: atypes.RoadHeight(index) + 1}, committee)
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

func TestGigaRouterCommon_ValidatorAddrUsesConfiguredBook(t *testing.T) {
	rng := utils.TestRng()
	validator := atypes.GenSecretKey(rng).Public()
	configured := GigaNodeAddr{
		Key:      makeKey(rng).Public(),
		HostPort: tcp.HostPort{Hostname: "configured.example", Port: 26656},
	}
	router := &GigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{validator: configured}},
	}
	got, ok := router.validatorAddr(validator)
	require.True(t, ok)
	require.NoError(t, utils.TestDiff(configured, got))
}

func TestGigaRouterCommon_runInboundConnAuthenticatesMemberAndClosesOnLeave(t *testing.T) {
	rng := utils.TestRng()
	validatorKey := atypes.GenSecretKey(rng)
	localKey := makeKey(rng)
	peerKey := makeKey(rng)
	selfAddr := NodeAddress{NodeID: peerKey.Public().NodeID(), Hostname: "validator.example", Port: 26656}
	dummy := atypes.GenSecretKey(rng)
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{validatorKey.Public(): 1}))
	app := abevm.NewProxy(newTestApp(testAppStateJSON(rng)))
	router, err := NewGigaFullnodeRouter(&GigaRouterCommonConfig{
		App:                     app,
		ValidatorAddrs:          map[atypes.PublicKey]GigaNodeAddr{},
		MaxInboundFullnodePeers: 10,
	}, localKey, epochs.state, newTestSigner(1, 0))
	require.NoError(t, err)
	cappedRouter, err := NewGigaFullnodeRouter(&GigaRouterCommonConfig{
		App:                     app,
		ValidatorAddrs:          map[atypes.PublicKey]GigaNodeAddr{},
		MaxInboundFullnodePeers: 0,
	}, localKey, epochs.state, newTestSigner(1, 0))
	require.NoError(t, err)
	gigaAddr := NodeAddress{NodeID: localKey.Public().NodeID(), Hostname: "giga.example", Port: 26656}
	joinerKey := makeKey(rng)
	outsider := handshakePairIn(t,
		[2]handshakeSpec{
			{
				NodeKey: localKey,
				Giga:    utils.Some(gigaHandshakeSpec{}),
			},
			{
				NodeKey: joinerKey,
				Giga: utils.Some(gigaHandshakeSpec{
					ValidatorKey: utils.Some(dummy),
					EVMRPC:       testEVMRPCURL(),
				}),
			},
		})
	// Router treats vaildators outside of committee as full nodes. And capped router rejects all full nodes.
	require.ErrorIs(t, cappedRouter.runInboundConn(t.Context(), outsider[0]), errInboundFullnodeLimit)
	_, ok := cappedRouter.validatorAddr(dummy.Public())
	require.False(t, ok)

	// Committee validators are accepted unconditionally.
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		pair, err := handshakePair(ctx, s, [2]handshakeSpec{
			{
				SelfAddr: utils.Some(gigaAddr),
				NodeKey:  localKey,
				Giga:     utils.Some(router.gigaSpec),
			},
			{
				SelfAddr: utils.Some(selfAddr),
				NodeKey:  peerKey,
				Giga: utils.Some(gigaHandshakeSpec{
					ValidatorKey: utils.Some(validatorKey),
					EVMRPC:       testEVMRPCURL(),
				}),
			},
		})
		if err != nil {
			return err
		}
		if err := utils.TestDiff(gigaAddr, pair[1].msg.SelfAddr.OrPanic("missing SelfAddr")); err != nil {
			return err
		}
		s.Spawn(func() error {
			if err, wantErr := router.runInboundConn(ctx, pair[0]), errGigaMembershipChanged; !errors.Is(err, wantErr) {
				return fmt.Errorf("runInboundConn() = nil, want %v", wantErr)
			}
			return nil
		})
		// Progress epoch, so that validator leaves committee and connection gets dropped.
		if err := router.poolInCommittee.WaitFor(ctx, validatorKey.Public()); err != nil {
			return err
		}
		epochs.store(testEpoch(2, map[atypes.PublicKey]uint64{dummy.Public(): 1}))
		return nil
	}))
}

func TestGigaRouterCommon_RunPerCommitteeMemberFollowsCommittee(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	b := atypes.GenSecretKey(rng).Public()
	epochs := newSettledEpochs(t, testEpoch(2, map[atypes.PublicKey]uint64{b: 1}))
	router := &GigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
		}},

		data:           epochs.state,
		validatorAddrs: testValidatorAddrs(epochs.state),
	}

	startedA := make(chan struct{}, 1)
	startedB := make(chan struct{}, 1)
	stoppedB := make(chan struct{}, 1)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, false, func(ctx context.Context, validator atypes.PublicKey, _ GigaNodeAddr) error {
				switch validator {
				case a:
					startedA <- struct{}{}
				case b:
					startedB <- struct{}{}
				}
				<-ctx.Done()
				if validator == b {
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
	router := &GigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
			c: {Key: makeKey(rng).Public()},
			d: {Key: makeKey(rng).Public()},
		}},
		data:           state,
		validatorAddrs: testValidatorAddrs(state),
	}

	started := make(chan atypes.PublicKey, 4)
	stoppedB := make(chan struct{}, 1)
	onStart := map[atypes.PublicKey]struct{}{}
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, false, func(ctx context.Context, validator atypes.PublicKey, _ GigaNodeAddr) error {
				started <- validator
				<-ctx.Done()
				if validator == b {
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

func TestGigaRouterCommon_RunPerCommitteeMemberLimitsToCommitEpoch(t *testing.T) {
	rng := utils.TestRng()
	keyA := atypes.GenSecretKey(rng)
	a, b := keyA.Public(), atypes.GenSecretKey(rng).Public()
	// ep2 = {a} is the commit epoch while the road block at LastRoad(2) is
	// uncertified. Certifying that road makes ep3 = {b} the commit epoch.
	// ep4 is registered up front: hashing LastRoad(2) would otherwise derive it
	// from execution weights that testCertifyRoad does not supply.
	registry := testRegistryThrough(t,
		map[atypes.PublicKey]uint64{a: 1},
		map[atypes.PublicKey]uint64{a: 1},
		map[atypes.PublicKey]uint64{b: 1},
		map[atypes.PublicKey]uint64{b: 1},
	)
	qc, blocks := testRoadQC(registry.MustEpoch(2), []atypes.SecretKey{keyA}, registry.LastRoad(2))
	state := testStateAtRoad(t, registry, qc, blocks)
	router := &GigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
		}},
		data:           state,
		validatorAddrs: testValidatorAddrs(state),
	}

	startedA := utils.NewAtomicSend(false)
	startedB := utils.NewAtomicSend(false)
	bCommitEpoch := utils.NewAtomicSend(atypes.EpochIndex(0))
	stoppedA := utils.NewAtomicSend(false)
	stoppedB := utils.NewAtomicSend(false)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, true, func(ctx context.Context, validator atypes.PublicKey, _ GigaNodeAddr) error {
				switch validator {
				case a:
					startedA.Store(true)
				case b:
					bCommitEpoch.Store(state.Anchor().Load().NextHeightEpoch().EpochIndex())
					startedB.Store(true)
				}
				<-ctx.Done()
				switch validator {
				case a:
					stoppedA.Store(true)
				case b:
					stoppedB.Store(true)
				}
				return ctx.Err()
			}))
		})
		if _, err := startedA.Wait(ctx, func(started bool) bool { return started }); err != nil {
			return err
		}
		if err := testCertifyRoad(ctx, state, rng, []atypes.SecretKey{keyA}, qc, blocks); err != nil {
			return err
		}
		// b is dialed only after the commit epoch is ep3. Dialing it under ep2 means
		// the dial set ignored currentCommitteeOnly.
		if _, err := startedB.Wait(ctx, func(started bool) bool { return started }); err != nil {
			return err
		}
		if got := bCommitEpoch.Load(); got != atypes.EpochIndex(3) {
			return fmt.Errorf("b dialed under commit epoch %d, want 3", got)
		}
		if _, err := stoppedA.Wait(ctx, func(stopped bool) bool { return stopped }); err != nil {
			return err
		}
		if stoppedB.Load() {
			return fmt.Errorf("b stopped after the commit moved")
		}
		return nil
	})
	require.NoError(t, err)
}

func TestGigaRouterCommon_RunPerCommitteeMemberDialsNextEpoch(t *testing.T) {
	rng := utils.TestRng()
	keyA, keyB := atypes.GenSecretKey(rng), atypes.GenSecretKey(rng)
	keyC := atypes.GenSecretKey(rng)
	a, b, c := keyA.Public(), keyB.Public(), keyC.Public()
	d := atypes.GenSecretKey(rng).Public()
	// Anchor starts at (ep2, ep3) = ({a,b}, {a,c}). Moving it to (ep3, ep4)
	// with ep4 = {c,d} drops b from both epochs and admits d as the next committee.
	registry := testRegistryThrough(t,
		map[atypes.PublicKey]uint64{a: 1, b: 1},
		map[atypes.PublicKey]uint64{a: 1, b: 1},
		map[atypes.PublicKey]uint64{a: 1, c: 1},
		map[atypes.PublicKey]uint64{c: 1, d: 1},
	)
	qc2, blocks2 := testRoadQC(registry.MustEpoch(2), []atypes.SecretKey{keyA, keyB}, registry.LastRoad(2))
	state := testStateAtRoad(t, registry, qc2, blocks2)
	router := &GigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
			c: {Key: makeKey(rng).Public()},
			d: {Key: makeKey(rng).Public()},
		}},
		data:           state,
		validatorAddrs: testValidatorAddrs(state),
	}

	started := map[atypes.PublicKey]*utils.AtomicSend[bool]{}
	stopped := map[atypes.PublicKey]*utils.AtomicSend[bool]{}
	var watches []utils.AtomicUpdate
	for _, k := range []atypes.PublicKey{a, b, c, d} {
		up, down := utils.NewAtomicSend(false), utils.NewAtomicSend(false)
		started[k], stopped[k] = &up, &down
		watches = append(watches, up.Subscribe(), down.Subscribe())
	}
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, false, func(ctx context.Context, validator atypes.PublicKey, _ GigaNodeAddr) error {
				started[validator].Store(true)
				<-ctx.Done()
				stopped[validator].Store(true)
				return ctx.Err()
			}))
		})
		if err := utils.WaitAny(ctx, func() bool {
			return started[a].Load() && started[b].Load() && started[c].Load()
		}, watches...); err != nil {
			return err
		}
		if started[d].Load() {
			return fmt.Errorf("d was dialed before the anchor moved to ep4")
		}
		// Certifying LastRoad(2) moves the commit epoch to ep3. Epochs[0] is still ep2, so b stays.
		if err := testCertifyRoad(ctx, state, rng, []atypes.SecretKey{keyA, keyB}, qc2, blocks2); err != nil {
			return err
		}
		for _, k := range []atypes.PublicKey{a, b, c, d} {
			if stopped[k].Load() {
				return fmt.Errorf("%v stopped while still in Epochs[0]", k)
			}
		}
		// The first road of ep3 moves Anchor to (ep3, ep4): b leaves, d joins.
		ep3 := registry.MustEpoch(3)
		qc3, blocks3 := data.TestCommitQCOnLane(ep3, []atypes.SecretKey{keyA, keyC}, utils.Some(qc2.QC()), ep3.Committee().Lanes().At(0))
		if err := testCertifyRoad(ctx, state, rng, []atypes.SecretKey{keyA, keyC}, qc3, blocks3); err != nil {
			return err
		}
		if _, err := stopped[b].Wait(ctx, func(stopped bool) bool { return stopped }); err != nil {
			return err
		}
		if _, err := started[d].Wait(ctx, func(started bool) bool { return started }); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
}

// testRegistryThrough returns a registry whose genesis epochs use genesis and whose
// epochs 2, 3, ... take their committees from later, in order.
func testRegistryThrough(t *testing.T, genesis map[atypes.PublicKey]uint64, later ...map[atypes.PublicKey]uint64) *epoch.TestRegistry {
	t.Helper()
	committee, err := atypes.NewCommittee(genesis)
	require.NoError(t, err)
	registry, err := epoch.TestGenesisRegistry(committee, 1, epoch.DefaultEpochLength)
	require.NoError(t, err)
	for i, weights := range later {
		// Epoch i+2 is derived when the last road of epoch i is reached.
		require.NoError(t, registry.TestTryPushEpoch(registry.LastRoad(atypes.EpochIndex(i)), weights))
	}
	return registry
}

// testRoadQC returns a commit QC for road of ep, signed by keys, with its road block.
func testRoadQC(ep *atypes.Epoch, keys []atypes.SecretKey, road atypes.RoadHeight) (*atypes.FullCommitQC, []*atypes.LaneBlock) {
	proposal := atypes.ProposalAt(ep, atypes.View{Height: road, Number: 0})
	block := atypes.NewLaneBlock(ep.Committee().Lanes().At(0), 0, atypes.LaneBlockHeaderHash{}, &atypes.Payload{})
	votes := make([]*atypes.Signed[*atypes.CommitVote], 0, len(keys))
	prepareQC := atypes.BuildPrepareQC(keys, proposal)
	for _, k := range keys {
		votes = append(votes, k.Sign(atypes.NewCommitVote(prepareQC)))
	}
	return atypes.NewFullCommitQC(atypes.NewCommitQC(votes), []*atypes.LaneBlockHeader{block.Header()}), []*atypes.LaneBlock{block}
}

// testStateAtRoad restarts a state over a block store whose only block is the road block of qc.
// Its Anchor is uncertified: the epochs around qc's road, and no AppQC is persisted yet.
func testStateAtRoad(t *testing.T, registry *epoch.TestRegistry, qc *atypes.FullCommitQC, blocks []*atypes.LaneBlock) *data.State {
	t.Helper()
	db := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	utils.OrPanic(db.WriteBlock(atypes.NewRoadBlock(qc, blocks)))
	utils.OrPanic(db.Flush())
	state, err := data.NewState(&data.Config{App: abevm.TestEpochApp(registry)}, db)
	require.NoError(t, err)
	return state
}

// testCertifyRoad pushes the road block of qc, then its AppHash and an AppQC signed by keys.
// The state persists the AppQC and publishes the moved Anchor, as it does in production.
func testCertifyRoad(ctx context.Context, state *data.State, rng utils.Rng, keys []atypes.SecretKey, qc *atypes.FullCommitQC, blocks []*atypes.LaneBlock) error {
	if err := state.PushPartialBlock(ctx, qc, data.TestBlockMap(blocks)); err != nil {
		return err
	}
	n := qc.Height()
	if err := state.PushAppHash(ctx, n, atypes.GenAppHash(rng)); err != nil {
		return err
	}
	vote, err := state.AppVote(ctx, n)
	if err != nil {
		return err
	}
	return state.PushAppQC(ctx, data.TestAppQC(keys, vote.Proposal()))
}

func TestInAnchorEpochsIncludesNextEpoch(t *testing.T) {
	rng := utils.TestRng()
	commitKey := atypes.GenSecretKey(rng)
	nextKey := atypes.GenSecretKey(rng)
	outsiderKey := atypes.GenSecretKey(rng)
	ep2 := testEpoch(2, map[atypes.PublicKey]uint64{commitKey.Public(): 1})
	ep3 := testEpoch(3, map[atypes.PublicKey]uint64{nextKey.Public(): 1})
	anchor := data.Anchor{Epochs: [2]*atypes.Epoch{ep2, ep3}}
	require.True(t, inAnchorEpochs(anchor, commitKey.Public()))
	require.True(t, inAnchorEpochs(anchor, nextKey.Public()))
	require.False(t, inAnchorEpochs(anchor, outsiderKey.Public()))
}

func TestGigaRouterCommon_RunPerCommitteeMemberRunsOneSessionPerMember(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	// b has no address-book entry, so an epoch containing only b starts nothing.
	b := atypes.GenSecretKey(rng).Public()
	epochs := newSettledEpochs(t, testEpoch(2, map[atypes.PublicKey]uint64{a: 1}))
	router := &GigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
		}},

		data:           epochs.state,
		validatorAddrs: testValidatorAddrs(epochs.state),
	}

	started := make(chan struct{}, 2)
	canceled := make(chan struct{}, 2)
	release := make(chan struct{})
	var live atomic.Int64
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, false, func(ctx context.Context, _ atypes.PublicKey, _ GigaNodeAddr) error {
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
	router := &GigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
		}},

		data:           epochs.state,
		validatorAddrs: testValidatorAddrs(epochs.state),
	}

	started := make(chan atypes.PublicKey, 2)
	canceled := make(chan struct{}, 2)
	release := make(chan struct{})
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, false, func(ctx context.Context, validator atypes.PublicKey, _ GigaNodeAddr) error {
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
	router := &GigaRouterCommon{data: state, validatorAddrs: testValidatorAddrs(state)}

	err := router.runUntilMembershipChange(t.Context(), a, func(context.Context) error {
		return nil
	})
	require.NoError(t, err)
}

func TestGigaRouterCommon_RunUntilMembershipChangeCancelsFWhenMembershipChanges(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	b := atypes.GenSecretKey(rng).Public()
	state := testDataWithAnchor(t, testAnchor(testEpoch(2, map[atypes.PublicKey]uint64{a: 1, b: 1})))
	router := &GigaRouterCommon{data: state, validatorAddrs: testValidatorAddrs(state)}
	started := make(chan struct{})
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			return router.runUntilMembershipChange(ctx, a, func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			})
		})
		<-started
		data.TestSetAnchor(state, testAnchor(testEpoch(3, map[atypes.PublicKey]uint64{b: 1})))
		return nil
	})
	require.ErrorIs(t, err, errGigaMembershipChanged)
}

// A peer that accepts and then says nothing must return from the handshake.
func TestGigaRouterCommon_OutboundHandshakeBoundedByTimeout(t *testing.T) {
	rng := utils.TestRng()
	addr := tcp.TestReserveAddr()
	listener, err := tcp.Listen(addr)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	router := &GigaRouterCommon{
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
		ConsensusParams: testGigaConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	db := blockstore.NewTestStore(t)
	cfg := &GigaRouterCommonConfig{
		DialInterval: time.Second,
		ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			validator.Public(): {Key: makeKey(rng).Public()},
		},
		PersistentStateDir:      t.TempDir(),
		App:                     abevm.NewProxy(&fixedHeightApp{height: 1}),
		AppHashBuilder:          newPublishingAppHashBuilder(),
		GenDoc:                  genDoc,
		MaxInboundFullnodePeers: 3,
		PersistentPeerIDs: map[tmtypes.NodeID]struct{}{
			persistentKeys[0].Public().NodeID(): {},
			persistentKeys[1].Public().NodeID(): {},
		},
	}
	registry := testGenesisEpochs(t, genDoc, cfg.ValidatorAddrs)
	state, err := BuildDataState(cfg, db, abevm.TestEpochApp(registry), genDoc.GenesisTime)
	require.NoError(t, err)
	router, err := NewGigaFullnodeRouter(cfg, localKey, state, newTestSigner(cfg.App.EvmChainID(), 0))
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
			hConn, err := handshake(connCtx, tcpConn, handshakeSpec{
				NodeKey:  peer,
				Giga:     utils.Some(gigaHandshakeSpec{}),
				IsServer: false,
			})
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

func (fixedHeightApp) EvmChainID() uint64 { return 1 }

func TestGigaRouterCommon_EvmProxyRejectsLocalAddrs(t *testing.T) {
	for _, endpoint := range []string{
		"ws://localhost:8546", "ws://127.0.0.1:8546", "ws://[::1]:8546",
		"ws://169.254.1.1:8546", "ws://[fe80::1]:8546", "ws://0.0.0.0:8546",
	} {
		t.Run(endpoint, func(t *testing.T) {
			rng := utils.TestRng()
			committee, keys := atypes.GenCommittee(rng, 1)
			state, _ := testDataState(t, committee)
			router, err := NewGigaFullnodeRouter(&GigaRouterCommonConfig{
				App: abevm.NewProxy(newTestApp(testAppStateJSON(rng))),
			}, makeKey(rng), state, newTestSigner(1, 0))
			require.NoError(t, err)
			require.NoError(t, router.runEvmProxy(t.Context(), validatorHandshake{
				auth:   keys[0].Sign(atypes.Challenge{}),
				EVMRPC: *utils.OrPanic1(url.Parse(endpoint)),
			}))
			for proxies := range router.proxies.RLock() {
				_, present := proxies[keys[0].Public()]
				require.False(t, present)
			}
		})
	}
}

func TestGigaRouterCommon_LoopbackEvmRPCDoesNotCreateProxy(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := atypes.GenCommittee(rng, 2)
	state, _ := testDataState(t, committee)
	validatorState, _ := testDataState(t, committee)
	validatorKey := keys[0]
	nodeKey := makeKey(rng)
	localNodeKey := makeKey(rng)
	localAddr := tcp.TestReserveAddr()
	genDoc := &tmtypes.GenesisDoc{ChainID: "loopback-evmrpc", InitialHeight: 1}
	require.NoError(t, genDoc.ValidateAndComplete())
	dir := t.TempDir()

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		addr := tcp.TestReserveAddr()
		s.SpawnBg(func() error {
			// Run an evmrpc server which is local.
			rpcServer := ethrpc.NewServer()
			defer rpcServer.Stop()
			httpServer := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
			defer httpServer.Close()
			endpoint := *utils.OrPanic1(url.Parse("ws" + strings.TrimPrefix(httpServer.URL, "http")))
			if !utils.IsLoopbackOrLinkLocalURL(endpoint) {
				return fmt.Errorf("need a local evmrpc for this test")
			}

			// Accept and run the fullnode and validator P2P connections.
			listener, err := tcp.Listen(addr)
			if err != nil {
				return fmt.Errorf("tcp.Listen(): %w", err)
			}
			defer func() { _ = listener.Close() }()
			for {
				conn, err := listener.AcceptOrClose(ctx)
				if err != nil {
					return utils.IgnoreCancel(err)
				}
				s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, conn.Run(ctx)) })
				hConn, err := handshake(ctx, conn, handshakeSpec{
					NodeKey: nodeKey,
					Giga: utils.Some(gigaHandshakeSpec{
						ValidatorKey: utils.Some(validatorKey),
						EVMRPC:       endpoint,
					}),
					IsServer: true,
				})
				if err != nil {
					return utils.IgnoreCancel(err)
				}
				s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, rpc.NewServer[giga.API]().Run(ctx, hConn.conn)) })
			}
		})
		cfg := GigaRouterCommonConfig{
			ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
				keys[1].Public(): {
					Key:      localNodeKey.Public(),
					HostPort: tcp.HostPort{Hostname: localAddr.Addr().String(), Port: localAddr.Port()},
				},
				validatorKey.Public(): {
					Key:      nodeKey.Public(),
					HostPort: tcp.HostPort{Hostname: addr.Addr().String(), Port: addr.Port()},
				},
			},
			DialInterval:       time.Second,
			App:                abevm.NewProxy(newTestApp(testAppStateJSON(rng))),
			AppHashBuilder:     newManualAppHashBuilder(),
			GenDoc:             genDoc,
			PersistentStateDir: dir,
			EnableEvmProxy:     true,
		}
		fullnodeRouter := utils.OrPanic1(NewGigaFullnodeRouter(&cfg, localNodeKey, state, newTestSigner(1, 0)))
		s.SpawnBg(func() error { return utils.IgnoreCancel(fullnodeRouter.Run(ctx)) })

		validatorRouter := utils.OrPanic1(NewGigaValidatorRouter(&GigaValidatorConfig{
			GigaRouterCommonConfig: cfg,
			ValidatorKey:           keys[1],
			EVMRPC:                 testEVMRPCURL(),
			ViewTimeout:            func(atypes.View) time.Duration { return time.Hour },
			ProposalTimeout:        time.Hour,
			Producer: &producer.Config{
				MaxGasWantedPerBlock:    1,
				MaxGasEstimatedPerBlock: 1,
				MaxTxsPerBlock:          1,
				MaxTxsPerSecond:         utils.None[uint64](),
				BlockInterval:           time.Second,
			},
		}, localNodeKey, validatorState, newTestSigner(1, 0)))
		s.SpawnBg(func() error { return utils.IgnoreCancel(validatorRouter.Run(ctx)) })

		// This test is not fully robust: these checks may be void in case runEvmProxy has not run yet,
		// even though the P2P connection is established.
		if err := fullnodeRouter.poolOut.WaitFor(ctx, validatorKey.Public()); err != nil {
			return err
		}
		for proxies := range fullnodeRouter.proxies.RLock() {
			if _, present := proxies[validatorKey.Public()]; present {
				return fmt.Errorf("fullnode EVM proxy created for loopback EVM RPC address")
			}
		}
		if err := validatorRouter.poolOut.WaitFor(ctx, validatorKey.Public()); err != nil {
			return err
		}
		for proxies := range validatorRouter.proxies.RLock() {
			if _, present := proxies[validatorKey.Public()]; present {
				return fmt.Errorf("validator EVM proxy created for loopback EVM RPC address")
			}
		}
		return nil
	}))
}

func TestGigaRouterCommon_EvmProxyClosesWithGigaConnection(t *testing.T) {
	rng := utils.TestRng()
	validatorKey := atypes.GenSecretKey(rng)
	peerKey := makeKey(rng)
	localKey := makeKey(rng)
	rpcServer := ethrpc.NewServer()
	t.Cleanup(rpcServer.Stop)
	opened := make(chan struct{})
	closed := make(chan struct{})
	wsHandler := rpcServer.WebsocketHandler([]string{"*"})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(opened)
		defer close(closed)
		wsHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	endpoint := *utils.OrPanic1(url.Parse("ws" + strings.TrimPrefix(httpServer.URL, "http")))

	addr := tcp.TestReserveAddr()
	listener, err := tcp.Listen(addr)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	router := &GigaRouterCommon{
		cfg:     &GigaRouterCommonConfig{DialInterval: time.Second, AllowLocalAddrs: true},
		key:     localKey,
		poolOut: giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
		proxies: utils.NewRWMutex(map[atypes.PublicKey]*ethrpc.Client{}),
	}
	disconnect := make(chan struct{})
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			c, err := listener.AcceptOrClose(ctx)
			if err != nil {
				return err
			}
			return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, serverScope scope.Scope) error {
				serverScope.SpawnBg(func() error { return c.Run(ctx) })
				hConn, err := handshake(ctx, c, handshakeSpec{
					NodeKey: peerKey,
					Giga: utils.Some(gigaHandshakeSpec{
						ValidatorKey: utils.Some(validatorKey),
						EVMRPC:       endpoint,
					}),
					IsServer: true,
				})
				if err != nil {
					return err
				}
				server := rpc.NewServer[giga.API]()
				serverScope.SpawnBg(func() error { return server.Run(ctx, hConn.conn) })
				_, _, err = utils.RecvOrClosed(ctx, disconnect)
				return err
			}))
		})
		finished := s.Spawn1(func() (struct{}, error) {
			err := router.dialAndRunConn(ctx, validatorKey.Public(), peerKey.Public(),
				tcp.HostPort{Hostname: addr.Addr().String(), Port: addr.Port()},
				func(ctx context.Context, _ rpc.Client[giga.API]) error {
					<-ctx.Done()
					return ctx.Err()
				})
			if err == nil {
				return struct{}{}, fmt.Errorf("giga connection ended without a disconnect error")
			}
			return struct{}{}, nil
		})
		if _, _, err := utils.RecvOrClosed(ctx, opened); err != nil {
			return err
		}
		close(disconnect)
		if _, err := finished.Join(ctx); err != nil {
			return err
		}
		_, _, err := utils.RecvOrClosed(ctx, closed)
		return err
	}))
	for proxies := range router.proxies.RLock() {
		_, present := proxies[validatorKey.Public()]
		require.False(t, present)
	}
}

// Retain reads the Anchor when it runs: a member admitted while a pass waits in stopStaleSessions
// survives that pass's Retain, even though the pass began under an Anchor that did not admit it.
func TestGigaRouterCommon_RetainReadsAnchorAtRetain(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng)
	x := atypes.GenSecretKey(rng)
	epochs := newSettledEpochs(t, testEpoch(1, map[atypes.PublicKey]uint64{a.Public(): 1}))
	validatorAddrs := testValidatorAddrs(epochs.state)
	router := &GigaRouterCommon{cfg: &GigaRouterCommonConfig{}, data: epochs.state, validatorAddrs: validatorAddrs}
	annA1 := testAddrAnnouncement(rng, a, 1, "a1.example:26656")
	annA2 := testAddrAnnouncement(rng, a, 2, "a2.example:26656")
	annX := testAddrAnnouncement(rng, x, 1, "x.example:26656")
	require.NoError(t, validatorAddrs.Receive(annA1))
	dialed := make(chan GigaNodeAddr, 4)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(router.runPerCommitteeMember(ctx, false, func(ctx context.Context, validator atypes.PublicKey, addr GigaNodeAddr) error {
				dialed <- addr
				if addr.HostPort.Hostname == "a1.example" {
					// Runs while the pass stops this session, after the pass has read the Anchor.
					<-ctx.Done()
					epochs.store(testEpoch(2, map[atypes.PublicKey]uint64{x.Public(): 1}))
					if err := validatorAddrs.Receive(annX); err != nil {
						return err
					}
				}
				<-ctx.Done()
				return ctx.Err()
			}))
		})
		require.Equal(t, "a1.example", (<-dialed).HostPort.Hostname)
		// The change of address stops a1's session, and the pass that does so admits x.
		require.NoError(t, validatorAddrs.Receive(annA2))
		// a is not dialed at its new address: the Anchor no longer admits it.
		require.Equal(t, "x.example", (<-dialed).HostPort.Hostname)
		return nil
	})
	require.NoError(t, err)
}

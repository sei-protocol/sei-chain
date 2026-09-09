package p2p

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
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

// newSeededVault returns a durable Pebble vault rooted in a temp dir with hash committed at height.
func newSeededVault(t *testing.T, height atypes.GlobalBlockNumber, hash []byte) hashvault.HashVault {
	t.Helper()
	cfg := hashvault.DefaultHashVaultConfig()
	cfg.DataDir = t.TempDir()
	v, err := hashvault.NewUnsafePebbleHashVault(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close(context.Background()) })
	require.NoError(t, v.CommitToHash(context.Background(), uint64(height), hash))
	return v
}

// TestCommitHashToVault covers the safety contract the restart path in runExecute relies on:
// an idempotent match returns nil, a divergent hash halts the node (panic), and a canceled
// context returns an error without halting.
func TestCommitHashToVault(t *testing.T) {
	const height atypes.GlobalBlockNumber = 42
	h1 := make([]byte, hashvault.BlockHashSize)
	for i := range h1 {
		h1[i] = 0xAA
	}
	h2 := make([]byte, hashvault.BlockHashSize)
	for i := range h2 {
		h2[i] = 0xBB
	}

	t.Run("matching hash is idempotent", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		require.NoError(t, commitAppHashToVault(context.Background(), vault, height, h1))
	})

	t.Run("divergent hash halts the node", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		require.Panics(t, func() {
			_ = commitAppHashToVault(context.Background(), vault, height, h2)
		})
	})

	t.Run("canceled context returns error without halting", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// Must not panic: a canceled context is a benign shutdown, not an equivocation.
		err := commitAppHashToVault(ctx, vault, height, h2)
		require.Error(t, err)
	})
}

func TestFinalizeBlockGasUsed(t *testing.T) {
	resp := &abci.ResponseFinalizeBlock{
		TxResults: []*abci.ExecTxResult{
			{GasUsed: 10},
			nil,
			{GasUsed: -1},
			{GasUsed: 20},
		},
	}
	require.Equal(t, int64(30), finalizeBlockGasUsed(resp))
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
	registry, err := epoch.NewRegistry(committee, atypes.GlobalBlockNumber(genDoc.InitialHeight), genDoc.GenesisTime, utils.None[string]())
	require.NoError(t, err)
	qc, blocks := data.TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*atypes.CommitQC]())
	gr := qc.QC().GlobalRange()
	require.Greater(t, gr.Len(), 2)
	last := gr.First + atypes.GlobalBlockNumber(gr.Len()/2)

	db, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	require.NoError(t, db.WriteQC(qc))
	for i, n := 0, gr.First; n < gr.Next; i, n = i+1, n+1 {
		require.NoError(t, db.WriteBlock(n, blocks[i]))
	}
	require.NoError(t, db.Flush())
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	state, err := BuildDataState(&GigaRouterCommonConfig{
		DialInterval:   time.Second,
		ValidatorAddrs: validatorAddrs,
		GenDoc:         genDoc,
		App:            proxy.New(&fixedHeightApp{height: int64(last)}),
	}, db)
	require.NoError(t, err)
	got, err := state.TryBlock(last)
	require.NoError(t, err)
	require.Equal(t, blocks[gr.Len()/2].Header().Hash(), got.Header().Hash())
}

func TestGigaRouterCommon_ValidatorsAtGlobalHeight(t *testing.T) {
	rng := utils.TestRng()
	low := atypes.GenSecretKey(rng)
	mid := atypes.GenSecretKey(rng)
	high := atypes.GenSecretKey(rng)
	keys := []atypes.SecretKey{low, mid, high}
	router := testGigaRouterWithData(t, map[atypes.PublicKey]GigaNodeAddr{
		low.Public():  {},
		mid.Public():  {},
		high.Public(): {},
	})
	first := router.data.Registry().FirstBlock()

	got, h, err := router.Validators(first)
	require.NoError(t, err)
	require.Equal(t, first, h)
	require.Len(t, got, 3)
	require.Equal(t, []int64{1, 1, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})

	_, _, err = router.Validators(0)
	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)

	_, _, err = router.Validators(first + 100)
	require.ErrorIs(t, err, coretypes.ErrHeightExceedsChainHead)

	weights := map[atypes.PublicKey]uint64{
		low.Public():  1,
		mid.Public():  5,
		high.Public(): 10,
	}
	require.NoError(t, router.data.Registry().StageAndActivate(0, weights))
	fakeNext := utils.NewAtomicSend(router.data.Registry().MustEpoch(2))
	router.nextCommitEpoch = fakeNext.Subscribe()
	got, h, err = router.Validators(first)
	require.NoError(t, err)
	require.Equal(t, first, h)
	require.Equal(t, []int64{1, 1, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})

	n := pushQCAtRoad(t, router, keys, router.data.Registry().MustEpoch(2), epoch.FirstRoad(2))
	got, h, err = router.Validators(n)
	require.NoError(t, err)
	require.Equal(t, n, h)
	require.Equal(t, []int64{10, 5, 1}, []int64{got[0].VotingPower, got[1].VotingPower, got[2].VotingPower})
	require.Equal(t, high.Public().Bytes(), got[0].PubKey.Bytes())
	require.Equal(t, mid.Public().Bytes(), got[1].PubKey.Bytes())
	require.Equal(t, low.Public().Bytes(), got[2].PubKey.Bytes())

	require.NoError(t, router.data.Registry().StageAndActivate(1, weights))
	require.NoError(t, router.data.Registry().StageAndActivate(2, weights))
	require.NoError(t, router.data.Registry().PruneBefore(4))
	_, _, err = router.Validators(n)
	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)
}

func pushQCAtRoad(t *testing.T, router *gigaRouterCommon, keys []atypes.SecretKey, ep *atypes.Epoch, road atypes.RoadIndex) atypes.GlobalBlockNumber {
	t.Helper()
	first := router.data.Registry().FirstBlock()
	proposal, blocks := atypes.ProposalAtBlocks(ep, atypes.View{Index: road, Number: 0}, first, 1)
	votes := make([]*atypes.Signed[*atypes.CommitVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, atypes.Sign(k, atypes.NewCommitVote(proposal)))
	}
	headers := make([]*atypes.BlockHeader, len(blocks))
	for i, b := range blocks {
		headers[i] = b.Header()
	}
	qc := atypes.NewFullCommitQC(atypes.NewCommitQC(votes), headers)
	require.NoError(t, router.data.PushQC(t.Context(), qc, blocks))
	return first
}

func testGigaRouterWithData(t *testing.T, addrs map[atypes.PublicKey]GigaNodeAddr) *gigaRouterCommon {
	t.Helper()
	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "validators-road-test",
		InitialHeight:   1,
		GenesisTime:     time.Now(),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	db, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	state, err := BuildDataState(&GigaRouterCommonConfig{
		DialInterval:   time.Second,
		ValidatorAddrs: addrs,
		GenDoc:         genDoc,
		App:            proxy.New(&fixedHeightApp{height: 1}),
	}, db)
	require.NoError(t, err)
	return &gigaRouterCommon{
		data:            state,
		nextCommitEpoch: state.NextCommitEpoch(),
	}
}

func testEpoch(index atypes.EpochIndex, weights map[atypes.PublicKey]uint64) *atypes.Epoch {
	committee := utils.OrPanic1(atypes.NewCommittee(weights))
	return atypes.NewEpoch(index, atypes.RoadRange{}, time.Time{}, committee, 1)
}

func testAnchor(ep *atypes.Epoch) utils.Option[data.Anchor] {
	return utils.Some(data.Anchor{Epoch: ep})
}

// settledEpochs drives both keep-set inputs to the same epoch. A one-epoch lag
// (commit ahead of Anchor) is sent to each watch separately.
type settledEpochs struct {
	commitEpoch utils.AtomicSend[*atypes.Epoch]
	anchor      utils.AtomicSend[utils.Option[data.Anchor]]
}

func newSettledEpochs(ep *atypes.Epoch) *settledEpochs {
	return &settledEpochs{
		commitEpoch: utils.NewAtomicSend(ep),
		anchor:      utils.NewAtomicSend(testAnchor(ep)),
	}
}

func (e *settledEpochs) store(ep *atypes.Epoch) {
	e.commitEpoch.Store(ep)
	e.anchor.Store(testAnchor(ep))
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
	epochs := newSettledEpochs(testEpoch(1, map[atypes.PublicKey]uint64{validatorKey.Public(): 1}))
	router := &gigaRouterCommon{
		cfg:             &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{}},
		nextCommitEpoch: epochs.commitEpoch.Subscribe(),
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
	evmRPC := *utils.OrPanic1(url.Parse("http://validator.example:8545"))
	dummy := atypes.GenSecretKey(rng)
	epochs := newSettledEpochs(testEpoch(1, map[atypes.PublicKey]uint64{validatorKey.Public(): 1}))
	genesis := map[atypes.PublicKey]GigaNodeAddr{dummy.Public(): {Key: makeKey(rng).Public()}}
	router := testGigaRouterWithData(t, genesis)
	router.nextCommitEpoch = epochs.commitEpoch.Subscribe()
	router.anchor = epochs.anchor.Subscribe()
	router.liveAddrs = utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{})
	router.liveAddrVersion = utils.NewAtomicSend(uint64(0))
	router.poolIn = giga.NewPool[NodePublicKey, rpc.Server[giga.API]]()
	router.poolInCommittee = giga.NewPool[atypes.PublicKey, rpc.Server[giga.API]]()
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
			router.anchor.Load(),
			router.nextCommitEpoch.Load(),
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
	epochs := newSettledEpochs(testEpoch(2, map[atypes.PublicKey]uint64{b: 1}))
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
		}},
		nextCommitEpoch: epochs.commitEpoch.Subscribe(),
		anchor:          epochs.anchor.Subscribe(),
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

func TestGigaRouterCommon_RunPerCommitteeMemberKeepsLeaversWhileAnchorLags(t *testing.T) {
	for _, tc := range []struct {
		name           string
		hasStartAnchor bool
	}{
		{"no AppQC yet", false},
		{"AppQC still in the prior epoch", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := utils.TestRng()
			a := atypes.GenSecretKey(rng).Public()
			b := atypes.GenSecretKey(rng).Public()
			c := atypes.GenSecretKey(rng).Public()
			ep1 := testEpoch(1, map[atypes.PublicKey]uint64{a: 1, b: 1})
			ep2 := testEpoch(2, map[atypes.PublicKey]uint64{a: 1, c: 1})
			startAnchor := utils.None[data.Anchor]()
			if tc.hasStartAnchor {
				startAnchor = testAnchor(ep1)
			}
			nextEpoch := utils.NewAtomicSend(ep1)
			anchor := utils.NewAtomicSend(startAnchor)
			router := &gigaRouterCommon{
				cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
					a: {Key: makeKey(rng).Public()},
					b: {Key: makeKey(rng).Public()},
					c: {Key: makeKey(rng).Public()},
				}},
				nextCommitEpoch: nextEpoch.Subscribe(),
				anchor:          anchor.Subscribe(),
				liveAddrs:       utils.NewRWMutex(map[atypes.PublicKey]GigaNodeAddr{}),
				liveAddrVersion: utils.NewAtomicSend(uint64(0)),
			}

			started := make(chan atypes.PublicKey, 3)
			stoppedB := make(chan struct{}, 1)
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
				for range 2 {
					<-started
				}
				nextEpoch.Store(ep2)
				<-started // c joined, so the prune for ep2 already ran
				select {
				case <-stoppedB:
					return fmt.Errorf("b stopped while Anchor was still catching up")
				default:
				}
				anchor.Store(testAnchor(ep2))
				<-stoppedB
				return nil
			})
			require.NoError(t, err)
		})
	}
}

func TestGigaRouterCommon_RunPerCommitteeMemberDialsBothCommitteesUntilStable(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	b := atypes.GenSecretKey(rng).Public()
	c := atypes.GenSecretKey(rng).Public()
	d := atypes.GenSecretKey(rng).Public()
	nextEpoch := utils.NewAtomicSend(testEpoch(4, map[atypes.PublicKey]uint64{a: 1, c: 1}))
	anchor := utils.NewAtomicSend(testAnchor(testEpoch(1, map[atypes.PublicKey]uint64{a: 1, b: 1})))
	lagAnchor := testEpoch(2, map[atypes.PublicKey]uint64{a: 1, c: 1, d: 1})
	dropAnchor := testEpoch(3, map[atypes.PublicKey]uint64{a: 1, c: 1})
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
			c: {Key: makeKey(rng).Public()},
			d: {Key: makeKey(rng).Public()},
		}},
		nextCommitEpoch: nextEpoch.Subscribe(),
		anchor:          anchor.Subscribe(),
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
		anchor.Store(testAnchor(lagAnchor))
		<-started // d joined, so the prune for lagAnchor already ran
		select {
		case <-stoppedB:
			return fmt.Errorf("b stopped while Anchor was still more than one epoch behind")
		default:
		}
		anchor.Store(testAnchor(dropAnchor))
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
	epochs := newSettledEpochs(testEpoch(2, map[atypes.PublicKey]uint64{a: 1}))
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
		}},
		nextCommitEpoch: epochs.commitEpoch.Subscribe(),
		anchor:          epochs.anchor.Subscribe(),
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
	epochs := newSettledEpochs(testEpoch(2, map[atypes.PublicKey]uint64{a: 1, b: 1}))
	router := &gigaRouterCommon{
		cfg: &GigaRouterCommonConfig{ValidatorAddrs: map[atypes.PublicKey]GigaNodeAddr{
			a: {Key: makeKey(rng).Public()},
			b: {Key: makeKey(rng).Public()},
		}},
		nextCommitEpoch: epochs.commitEpoch.Subscribe(),
		anchor:          epochs.anchor.Subscribe(),
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
	nextEpoch := utils.NewAtomicSend(testEpoch(2, map[atypes.PublicKey]uint64{a: 1}))
	router := &gigaRouterCommon{nextCommitEpoch: nextEpoch.Subscribe()}

	changed, err := router.runUntilMembershipChange(t.Context(), a, true, func(context.Context) error {
		return nil
	})
	require.NoError(t, err)
	require.False(t, changed)
}

func TestGigaRouterCommon_RunUntilMembershipChangeCancelsFWhenMembershipChanges(t *testing.T) {
	rng := utils.TestRng()
	a := atypes.GenSecretKey(rng).Public()
	b := atypes.GenSecretKey(rng).Public()
	nextEpoch := utils.NewAtomicSend(testEpoch(2, map[atypes.PublicKey]uint64{a: 1, b: 1}))
	router := &gigaRouterCommon{nextCommitEpoch: nextEpoch.Subscribe()}
	started := make(chan struct{})
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			changed, err := router.runUntilMembershipChange(ctx, a, true, func(ctx context.Context) error {
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
		nextEpoch.Store(testEpoch(3, map[atypes.PublicKey]uint64{b: 1}))
		return nil
	})
	require.NoError(t, err)
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

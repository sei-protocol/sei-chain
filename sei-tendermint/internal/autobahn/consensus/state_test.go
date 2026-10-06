package consensus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func hourTimeout(types.View) time.Duration { return time.Hour }

func secretKeyForView(reg *epoch.Registry, keys []types.SecretKey, view types.View) types.SecretKey {
	want := reg.MustEpoch(0).Committee().Leader(view)
	for _, k := range keys {
		if k.Public() == want {
			return k
		}
	}
	panic("leader key not in committee")
}

type testKeyPick = func(*epoch.Registry, []types.SecretKey) types.SecretKey

// newTestState returns a State with no persistence and an hour view timeout.
// keys[0] is the node's signing key.
func newTestState(rng utils.Rng) (*State, []types.SecretKey, *epoch.Registry) {
	return newTestStateWith(rng, utils.None[testKeyPick](), utils.None[ViewTimeoutFunc]())
}

func newTestStateWith(
	rng utils.Rng,
	pick utils.Option[testKeyPick],
	timeout utils.Option[ViewTimeoutFunc],
) (*State, []types.SecretKey, *epoch.Registry) {
	registry, keys := epoch.GenRegistry(rng, 3)
	key := keys[0]
	if pick, ok := pick.Get(); ok {
		key = pick(registry, keys)
	}
	s := utils.OrPanic1(NewState(&Config{
		Key:                key,
		ViewTimeout:        timeout.Or(hourTimeout),
		PersistentStateDir: utils.None[string](),
	}, newTestDataState(registry)))
	return s, keys, registry
}

// makeTimeoutQC returns a TimeoutQC for view in which every vote carries pqc.
func makeTimeoutQC(keys []types.SecretKey, view types.View, pqc utils.Option[*types.PrepareQC]) *types.TimeoutQC {
	votes := make([]*types.FullTimeoutVote, len(keys))
	for i, k := range keys {
		votes[i] = types.NewFullTimeoutVote(k, view, pqc)
	}
	return types.NewTimeoutQC(votes)
}

// testTimeoutVotePrepareQC returns the PrepareQC carried by tv.
func testTimeoutVotePrepareQC(tv *types.FullTimeoutVote) utils.Option[*types.PrepareQC] {
	return types.NewTimeoutQC([]*types.FullTimeoutVote{tv}).LatestPrepareQC()
}

func makeFullProposal(rng utils.Rng, keys []types.SecretKey, vs types.ViewSpec, valid bool) *types.FullProposal {
	return makeFullProposalAt(rng, keys, vs, vs.NextTimestamp(), valid)
}

func makeFullProposalAt(rng utils.Rng, keys []types.SecretKey, vs types.ViewSpec, ts time.Time, valid bool) *types.FullProposal {
	c := vs.Epoch.Committee()
	lane := c.Lanes().At(0)
	prev := types.LaneRangeOpt(vs.CommitQC, lane)
	header := types.NewBlock(lane, prev.Next(), prev.LastHash(), &types.Payload{}).Header()
	laneVote := types.NewLaneVote(header)
	laneVotes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
	for _, key := range types.TestKeysWithWeight(c, keys, c.LaneQuorum()) {
		laneVotes = append(laneVotes, types.Sign(key, laneVote))
	}
	laneQCs := map[types.LaneID]*types.LaneQC{lane: types.NewLaneQC(laneVotes)}

	if !valid {
		return utils.OrPanic1(types.NewProposalForTesting(c, vs, ts, laneQCs, types.GenSignature(rng)))
	}
	leader := c.Leader(vs.View())
	for _, key := range keys {
		if key.Public() == leader {
			return utils.OrPanic1(types.NewProposal(key, vs, ts, laneQCs))
		}
	}
	panic("leader key not found")
}

type liveEnv struct {
	t    *testing.T
	rng  utils.Rng
	s    *State
	keys []types.SecretKey
	reg  *epoch.Registry
	vs   types.ViewSpec
}

func newLiveEnv(t *testing.T, rng utils.Rng) liveEnv {
	t.Helper()
	return newLiveEnvWith(t, rng, utils.None[testKeyPick](), utils.None[ViewTimeoutFunc]())
}

func newLiveEnvWith(
	t *testing.T,
	rng utils.Rng,
	pick utils.Option[testKeyPick],
	timeout utils.Option[ViewTimeoutFunc],
) liveEnv {
	t.Helper()
	s, keys, reg := newTestStateWith(rng, pick, timeout)
	return liveEnv{t: t, rng: rng, s: s, keys: keys, reg: reg, vs: s.myView.Load()}
}

func (e liveEnv) committee() *types.Committee { return e.vs.Epoch.Committee() }

func (e liveEnv) proposal() *types.Proposal {
	return types.GenProposalForEpoch(e.rng, e.vs.Epoch, e.vs.View())
}

func (e liveEnv) outsiderKeys() []types.SecretKey {
	keys := make([]types.SecretKey, len(e.keys))
	for i := range keys {
		keys[i] = types.GenSecretKey(e.rng)
	}
	return keys
}

func (e liveEnv) signers(valid bool) []types.SecretKey {
	if valid {
		return e.keys
	}
	return e.outsiderKeys()
}

func (e liveEnv) inner() inner { return e.s.innerRecv.Load() }

func (e liveEnv) pushProposal(ts time.Time) {
	e.t.Helper()
	require.NoError(e.t, e.s.PushProposal(e.t.Context(), makeFullProposalAt(e.rng, e.keys, e.vs, ts, true)))
}

func (e liveEnv) pushPrepareQC() *types.PrepareQC {
	e.t.Helper()
	qc := makePrepareQC(e.keys, e.proposal())
	require.NoError(e.t, e.s.pushPrepareQC(e.t.Context(), qc))
	return qc
}

func (e liveEnv) occupyBusy() {
	e.t.Helper()
	e.pushProposal(e.vs.NextTimestamp())
	e.pushPrepareQC()
}

func (e liveEnv) voteTimeout() {
	e.t.Helper()
	require.NoError(e.t, e.s.voteTimeout(e.t.Context(), e.vs.View()))
}

func (e liveEnv) seedLaneQC(ctx context.Context) error {
	av := e.s.Avail()
	lane, ok := av.LocalLane().Get()
	if !ok {
		return fmt.Errorf("local lane missing")
	}
	b, err := av.ProduceLocalBlock(lane, 0, &types.Payload{})
	if err != nil {
		return fmt.Errorf("ProduceLocalBlock: %w", err)
	}
	h := b.Msg().Block().Header()
	c := e.committee()
	for _, k := range types.TestKeysWithWeight(c, e.keys, c.LaneQuorum()) {
		if err := av.PushVote(ctx, types.Sign(k, types.NewLaneVote(h))); err != nil {
			return fmt.Errorf("PushVote: %w", err)
		}
	}
	return nil
}

func (e liveEnv) spawnRun(ctx context.Context, sc scope.Scope) {
	sc.SpawnBg(func() error { return utils.IgnoreCancel(e.s.Data().Run(ctx)) })
	sc.SpawnBg(func() error { return utils.IgnoreCancel(e.s.Run(ctx)) })
}

func requireNoPerView(t *testing.T, i inner) {
	t.Helper()
	require.False(t, i.PrepareVote.IsPresent())
	require.False(t, i.CommitVote.IsPresent())
	require.False(t, i.TimeoutVote.IsPresent())
	require.False(t, i.PrepareQC.IsPresent())
	require.False(t, i.TimeoutQC.IsPresent())
}

func advancingSpec(keys []types.SecretKey, registry *epoch.Registry) types.ConsensusSpec {
	ep0 := registry.MustEpoch(0)
	qc := types.BuildCommitQC(ep0, keys, utils.None[*types.CommitQC](), nil)
	return types.ConsensusSpec{
		CommitQC: utils.Some(qc),
		Epoch:    utils.OrPanic1(registry.EpochAt(qc.Index() + 1)),
	}
}

// --- wait / verify / store (proposal, prepare QC, timeout QC) ---

type waitVerifyPushCase struct {
	name          string
	push          func(liveEnv, bool) error
	assertApplied func(liveEnv)
	assertIgnored func(liveEnv)
	// pushStale is the message delivered after the view has moved on.
	// None reuses push with a valid message.
	pushStale utils.Option[func(context.Context, liveEnv) error]
	// assertStale checks the state after pushStale. None reuses assertIgnored.
	assertStale utils.Option[func(liveEnv)]
}

func waitVerifyPushCases() []waitVerifyPushCase {
	return []waitVerifyPushCase{
		{
			name: "proposal",
			push: func(e liveEnv, valid bool) error {
				return e.s.PushProposal(e.t.Context(), makeFullProposal(e.rng, e.keys, e.vs, valid))
			},
			assertApplied: func(e liveEnv) {
				vote, ok := e.inner().PrepareVote.Get()
				require.True(e.t, ok)
				require.Equal(e.t, e.vs.View(), vote.Msg().Proposal().View())
			},
			assertIgnored: func(e liveEnv) {
				require.False(e.t, e.inner().PrepareVote.IsPresent())
			},
		},
		{
			name: "prepare QC",
			push: func(e liveEnv, valid bool) error {
				return e.s.pushPrepareQC(e.t.Context(), makePrepareQC(e.signers(valid), e.proposal()))
			},
			assertApplied: func(e liveEnv) {
				i := e.inner()
				qc, ok := i.PrepareQC.Get()
				require.True(e.t, ok)
				require.Equal(e.t, e.vs.View(), qc.Proposal().View())
				vote, ok := i.CommitVote.Get()
				require.True(e.t, ok)
				require.Equal(e.t, e.vs.View(), vote.Msg().Proposal().View())
			},
			assertIgnored: func(e liveEnv) {
				i := e.inner()
				require.False(e.t, i.PrepareQC.IsPresent())
				require.False(e.t, i.CommitVote.IsPresent())
			},
		},
		{
			name: "timeout QC",
			push: func(e liveEnv, valid bool) error {
				return e.s.PushTimeoutQC(e.t.Context(), makeTimeoutQC(e.signers(valid), e.vs.View(), utils.None[*types.PrepareQC]()))
			},
			assertApplied: func(e liveEnv) {
				qc, ok := e.inner().TimeoutQC.Get()
				require.True(e.t, ok)
				require.Equal(e.t, e.vs.View(), qc.View())
			},
			assertIgnored: func(e liveEnv) {
				require.False(e.t, e.inner().TimeoutQC.IsPresent())
			},
			pushStale: utils.Some(func(ctx context.Context, e liveEnv) error {
				pqc := makePrepareQC(e.keys, e.proposal())
				return e.s.PushTimeoutQC(ctx, makeTimeoutQC(e.keys, e.vs.View(), utils.Some(pqc)))
			}),
			assertStale: utils.Some(func(e liveEnv) {
				qc, ok := e.inner().TimeoutQC.Get()
				require.True(e.t, ok)
				require.False(e.t, qc.LatestPrepareQC().IsPresent())
			}),
		},
	}
}

func TestWaitVerifyPush(t *testing.T) {
	for _, tc := range waitVerifyPushCases() {
		t.Run(tc.name+"/applies current", func(t *testing.T) {
			e := newLiveEnv(t, utils.TestRng())
			require.NoError(t, tc.push(e, true))
			tc.assertApplied(e)
		})

		t.Run(tc.name+"/rejects invalid", func(t *testing.T) {
			e := newLiveEnv(t, utils.TestRng())
			require.Error(t, tc.push(e, false))
			tc.assertIgnored(e)
		})

		t.Run(tc.name+"/ignores stale", func(t *testing.T) {
			e := newLiveEnv(t, utils.TestRng())
			var current types.View
			err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
				sc.SpawnBg(func() error { return utils.IgnoreCancel(e.s.Run(ctx)) })
				if err := e.s.PushTimeoutQC(ctx, makeTimeoutQC(e.keys, e.vs.View(), utils.None[*types.PrepareQC]())); err != nil {
					return fmt.Errorf("advance view: %w", err)
				}
				vs, err := e.s.myView.Wait(ctx, func(vs types.ViewSpec) bool {
					return e.vs.View().Less(vs.View())
				})
				if err != nil {
					return err
				}
				current = vs.View()
				if pushStale, ok := tc.pushStale.Get(); ok {
					return pushStale(ctx, e)
				}
				return tc.push(e, true)
			})
			require.NoError(t, err)
			require.Equal(t, current, e.inner().View())
			if assertStale, ok := tc.assertStale.Get(); ok {
				assertStale(e)
			} else {
				tc.assertIgnored(e)
			}
		})
	}
}

func TestPushProposalAheadWaitsForView(t *testing.T) {
	e := newLiveEnv(t, utils.TestRng())
	future := types.ViewSpec{
		ConsensusSpec: e.vs.ConsensusSpec,
		TimeoutQC:     utils.Some(makeTimeoutQC(e.keys, e.vs.View(), utils.None[*types.PrepareQC]())),
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, e.s.PushProposal(ctx, makeFullProposal(e.rng, e.keys, future, true)), context.Canceled)
	require.False(t, e.inner().PrepareVote.IsPresent())
}

// --- occupied inner (second action is a no-op or a wipe) ---

func TestOccupiedInner(t *testing.T) {
	t.Run("second proposal ignored", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		e.pushProposal(e.vs.NextTimestamp())
		first, ok := e.inner().PrepareVote.Get()
		require.True(t, ok)

		e.pushProposal(e.vs.NextTimestamp().Add(time.Second))
		got, ok := e.inner().PrepareVote.Get()
		require.True(t, ok)
		require.Equal(t, first, got)
	})

	t.Run("second prepare QC ignored", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		first := e.pushPrepareQC()
		e.pushPrepareQC()
		got, ok := e.inner().PrepareQC.Get()
		require.True(t, ok)
		require.Equal(t, first, got)
	})

	t.Run("second timeout vote ignored", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		e.voteTimeout()
		first, ok := e.inner().TimeoutVote.Get()
		require.True(t, ok)

		e.voteTimeout()
		got, ok := e.inner().TimeoutVote.Get()
		require.True(t, ok)
		require.Equal(t, first, got)
	})

	t.Run("timeout vote ignores proposal", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		e.voteTimeout()
		e.pushProposal(e.vs.NextTimestamp())
		require.True(t, e.inner().TimeoutVote.IsPresent())
		require.False(t, e.inner().PrepareVote.IsPresent())
	})

	t.Run("timeout vote ignores prepare QC", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		e.voteTimeout()
		e.pushPrepareQC()
		require.True(t, e.inner().TimeoutVote.IsPresent())
		require.False(t, e.inner().PrepareQC.IsPresent())
		require.False(t, e.inner().CommitVote.IsPresent())
	})

	t.Run("timeout QC clears per-view state", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		e.occupyBusy()
		require.NoError(t, e.s.PushTimeoutQC(e.t.Context(), makeTimeoutQC(e.keys, e.vs.View(), utils.None[*types.PrepareQC]())))

		i := e.inner()
		qc, ok := i.TimeoutQC.Get()
		require.True(t, ok)
		require.Equal(t, e.vs.View(), qc.View())
		require.Equal(t, e.vs.View().Next(), i.View())
		require.False(t, i.PrepareVote.IsPresent())
		require.False(t, i.CommitVote.IsPresent())
		require.False(t, i.TimeoutVote.IsPresent())
		require.False(t, i.PrepareQC.IsPresent())
	})

	t.Run("pushSpec clears per-view state", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		e.occupyBusy()
		spec := advancingSpec(e.keys, e.reg)
		require.NoError(t, e.s.pushSpec(spec))

		i := e.inner()
		require.Equal(t, spec.Index(), i.Index)
		require.Equal(t, spec.Epoch.EpochIndex(), i.spec.Epoch.EpochIndex())
		requireNoPerView(t, i)
	})

	t.Run("pushSpec ignores non-advancing spec", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		e.pushProposal(e.vs.NextTimestamp())
		before, ok := e.inner().PrepareVote.Get()
		require.True(t, ok)

		require.NoError(t, e.s.pushSpec(e.inner().spec))
		got, ok := e.inner().PrepareVote.Get()
		require.True(t, ok)
		require.Equal(t, before, got)
	})
}

// --- vote ingest (prepare, commit, timeout) ---

type pushVoteCase struct {
	name   string
	quorum func(*types.Committee) uint64
	push   func(liveEnv, types.SecretKey, *types.Proposal) error
	badSig utils.Option[func(liveEnv, *types.Proposal) error]
	qcView func(*State) (types.View, bool)
}

func fakeSig(key types.PublicKey) *types.Signature {
	return utils.OrPanic1(types.SignatureForTesting(key, make([]byte, 64)))
}

func pushVoteCases() []pushVoteCase {
	return []pushVoteCase{
		{
			name:   "prepare",
			quorum: (*types.Committee).PrepareQuorum,
			push: func(e liveEnv, k types.SecretKey, p *types.Proposal) error {
				return e.s.PushPrepareVote(types.Sign(k, types.NewPrepareVote(p)))
			},
			badSig: utils.Some(func(e liveEnv, p *types.Proposal) error {
				return e.s.PushPrepareVote(types.SignedForTesting(types.NewPrepareVote(p), fakeSig(e.keys[0].Public())))
			}),
			qcView: func(s *State) (types.View, bool) {
				qc, ok := s.prepareQC().Load().Get()
				if !ok {
					return types.View{}, false
				}
				return qc.Proposal().View(), true
			},
		},
		{
			name:   "commit",
			quorum: (*types.Committee).CommitQuorum,
			push: func(e liveEnv, k types.SecretKey, p *types.Proposal) error {
				return e.s.PushCommitVote(types.Sign(k, types.NewCommitVote(p)))
			},
			badSig: utils.Some(func(e liveEnv, p *types.Proposal) error {
				return e.s.PushCommitVote(types.SignedForTesting(types.NewCommitVote(p), fakeSig(e.keys[0].Public())))
			}),
			qcView: func(s *State) (types.View, bool) {
				qc, ok := s.commitQC().Load().Get()
				if !ok {
					return types.View{}, false
				}
				return qc.Proposal().View(), true
			},
		},
		{
			name:   "timeout",
			quorum: (*types.Committee).TimeoutQuorum,
			push: func(e liveEnv, k types.SecretKey, _ *types.Proposal) error {
				return e.s.PushTimeoutVote(types.NewFullTimeoutVote(k, e.vs.View(), utils.None[*types.PrepareQC]()))
			},
			qcView: func(s *State) (types.View, bool) {
				qc, ok := s.timeoutQC().Load().Get()
				if !ok {
					return types.View{}, false
				}
				return qc.View(), true
			},
		},
	}
}

func TestPushVote(t *testing.T) {
	for _, tc := range pushVoteCases() {
		t.Run(tc.name+"/forms QC", func(t *testing.T) {
			e := newLiveEnv(t, utils.TestRng())
			c := e.committee()
			p := e.proposal()
			require.NoError(t, tc.push(e, e.keys[0], p))
			_, ok := tc.qcView(e.s)
			require.False(t, ok)

			for _, k := range types.TestKeysWithWeight(c, e.keys, tc.quorum(c)) {
				require.NoError(t, tc.push(e, k, p))
			}
			view, ok := tc.qcView(e.s)
			require.True(t, ok)
			require.Equal(t, e.vs.View(), view)
		})

		t.Run(tc.name+"/rejects non-replica", func(t *testing.T) {
			e := newLiveEnv(t, utils.TestRng())
			require.Error(t, tc.push(e, types.GenSecretKey(e.rng), e.proposal()))
			_, ok := tc.qcView(e.s)
			require.False(t, ok)
		})

		if badSig, ok := tc.badSig.Get(); ok {
			t.Run(tc.name+"/rejects bad sig", func(t *testing.T) {
				e := newLiveEnv(t, utils.TestRng())
				require.Error(t, badSig(e, e.proposal()))
			})
		}
	}

	t.Run("timeout/rejects wrong epoch", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		view := e.vs.View()
		view.EpochIndex++
		require.Error(t, e.s.PushTimeoutVote(types.NewFullTimeoutVote(e.keys[0], view, utils.None[*types.PrepareQC]())))
	})
}

// --- propose ---

func TestRunPropose(t *testing.T) {
	t.Run("fresh proposal", func(t *testing.T) {
		rng := utils.TestRng()
		e := newLiveEnvWith(t, rng, utils.Some(testKeyPick(func(reg *epoch.Registry, keys []types.SecretKey) types.SecretKey {
			return secretKeyForView(reg, keys, types.View{})
		})), utils.None[ViewTimeoutFunc]())
		require.Equal(t, e.s.cfg.Key.Public(), e.committee().Leader(e.vs.View()))

		err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
			if err := e.seedLaneQC(ctx); err != nil {
				return err
			}
			sc.SpawnBg(func() error { return utils.IgnoreCancel(e.s.runPropose(ctx)) })
			got, err := e.s.SubscribeProposal().Wait(ctx, func(o utils.Option[*types.FullProposal]) bool {
				return o.IsPresent()
			})
			if err != nil {
				return err
			}
			p, ok := got.Get()
			if !ok {
				return fmt.Errorf("proposal missing")
			}
			if p.View() != e.vs.View() {
				return fmt.Errorf("proposal view %v, want %v", p.View(), e.vs.View())
			}
			if err := p.Verify(e.vs); err != nil {
				return fmt.Errorf("proposal.Verify(): %w", err)
			}
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("reproposal", func(t *testing.T) {
		rng := utils.TestRng()
		next := types.View{}.Next()
		e := newLiveEnvWith(t, rng, utils.Some(testKeyPick(func(reg *epoch.Registry, keys []types.SecretKey) types.SecretKey {
			return secretKeyForView(reg, keys, next)
		})), utils.None[ViewTimeoutFunc]())
		e.occupyBusy()
		require.NoError(t, e.s.PushTimeoutQC(t.Context(), makeTimeoutQC(e.keys, e.vs.View(), e.inner().PrepareQC)))

		err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
			sc.SpawnBg(func() error { return utils.IgnoreCancel(e.s.runOutputs(ctx)) })
			vs, err := e.s.myView.Wait(ctx, func(vs types.ViewSpec) bool {
				return e.vs.View().Less(vs.View())
			})
			if err != nil {
				return err
			}
			if vs.Epoch.Committee().Leader(vs.View()) != e.s.cfg.Key.Public() {
				return fmt.Errorf("not leader of repropose view")
			}
			sc.SpawnBg(func() error { return utils.IgnoreCancel(e.s.runPropose(ctx)) })
			got, err := e.s.SubscribeProposal().Wait(ctx, func(o utils.Option[*types.FullProposal]) bool {
				return o.IsPresent()
			})
			if err != nil {
				return err
			}
			p, ok := got.Get()
			if !ok {
				return fmt.Errorf("proposal missing")
			}
			if p.View() != vs.View() {
				return fmt.Errorf("proposal view %v, want %v", p.View(), vs.View())
			}
			if !p.TimeoutQC().IsPresent() {
				return fmt.Errorf("reproposal missing TimeoutQC")
			}
			if err := p.Verify(vs); err != nil {
				return fmt.Errorf("proposal.Verify(): %w", err)
			}
			return nil
		})
		require.NoError(t, err)
	})
}

// --- Run wiring ---

func TestRun(t *testing.T) {
	t.Run("applies aggregated prepare QC", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		p := makeFullProposal(e.rng, e.keys, e.vs, true).Proposal().Msg()
		err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
			e.spawnRun(ctx, sc)
			c := e.committee()
			for _, k := range types.TestKeysWithWeight(c, e.keys, c.PrepareQuorum()) {
				if err := e.s.PushPrepareVote(types.Sign(k, types.NewPrepareVote(p))); err != nil {
					return err
				}
			}
			_, err := e.s.innerRecv.Wait(ctx, func(i inner) bool { return i.PrepareQC.IsPresent() })
			return err
		})
		require.NoError(t, err)
	})

	t.Run("applies aggregated timeout QC", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
			e.spawnRun(ctx, sc)
			c := e.committee()
			for _, k := range types.TestKeysWithWeight(c, e.keys, c.TimeoutQuorum()) {
				if err := e.s.PushTimeoutVote(types.NewFullTimeoutVote(k, e.vs.View(), utils.None[*types.PrepareQC]())); err != nil {
					return err
				}
			}
			_, err := e.s.innerRecv.Wait(ctx, func(i inner) bool { return i.TimeoutQC.IsPresent() })
			return err
		})
		require.NoError(t, err)
	})

	t.Run("stores aggregated commit QC in avail", func(t *testing.T) {
		e := newLiveEnv(t, utils.TestRng())
		p := makeFullProposal(e.rng, e.keys, e.vs, true).Proposal().Msg()
		err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
			e.spawnRun(ctx, sc)
			c := e.committee()
			for _, k := range types.TestKeysWithWeight(c, e.keys, c.PrepareQuorum()) {
				if err := e.s.PushPrepareVote(types.Sign(k, types.NewPrepareVote(p))); err != nil {
					return err
				}
			}
			if _, err := e.s.innerRecv.Wait(ctx, func(i inner) bool { return i.PrepareQC.IsPresent() }); err != nil {
				return err
			}
			for _, k := range types.TestKeysWithWeight(c, e.keys, c.CommitQuorum()) {
				if err := e.s.PushCommitVote(types.Sign(k, types.NewCommitVote(p))); err != nil {
					return err
				}
			}
			_, err := e.s.Avail().SubscribeConsensusSpec().Wait(ctx, func(sp types.ConsensusSpec) bool {
				return sp.Index() > e.vs.View().Index
			})
			return err
		})
		require.NoError(t, err)
	})

	t.Run("vote timeout after view timeout", func(t *testing.T) {
		e := newLiveEnvWith(t, utils.TestRng(), utils.None[testKeyPick](), utils.Some(ViewTimeoutFunc(func(types.View) time.Duration { return 0 })))
		err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
			e.spawnRun(ctx, sc)
			_, err := e.s.innerRecv.Wait(ctx, func(i inner) bool { return i.TimeoutVote.IsPresent() })
			return err
		})
		require.NoError(t, err)
	})
}

// --- voteTimeout PrepareQC selection tests ---
//
// These exercise the real State.pushTimeoutQC and State.voteTimeout methods
// rather than mirroring their logic, covering five scenarios:
//   1. Both None                           → None
//   2. i.PrepareQC present, no TimeoutQC   → uses PrepareQC
//   3. i.PrepareQC None, inherited present → inherited (consecutive timeout / offline leader)
//   4. Both present, current view higher   → uses current PrepareQC
//   5. i.PrepareQC present, inherited None → uses PrepareQC

func TestVoteTimeoutPrepareQC_BothNone(t *testing.T) {
	rng := utils.TestRng()
	s, _, _ := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		if err := s.voteTimeout(ctx, types.View{Index: 0, Number: 0}); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present")
		}
		if testTimeoutVotePrepareQC(tv).IsPresent() {
			return fmt.Errorf("PrepareQC should not be present")
		}
		return nil
	})
	require.NoError(t, err)
}

func TestVoteTimeoutPrepareQC_OnlyCurrentView(t *testing.T) {
	rng := utils.TestRng()
	s, keys, registry := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		pqc := makePrepareQC(keys, types.GenProposalForEpoch(rng, registry.MustEpoch(0), types.View{Index: 0, Number: 0}))
		if err := s.pushPrepareQC(ctx, pqc); err != nil {
			return fmt.Errorf("pushPrepareQC: %w", err)
		}
		if err := s.voteTimeout(ctx, types.View{Index: 0, Number: 0}); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present")
		}
		if !testTimeoutVotePrepareQC(tv).IsPresent() {
			return fmt.Errorf("PrepareQC should be present")
		}
		return nil
	})
	require.NoError(t, err)
}

// TestVoteTimeoutPrepareQC_InheritedFromTimeoutQC is the core safety test:
// consecutive timeouts with an offline leader must not lose the PrepareQC.
func TestVoteTimeoutPrepareQC_InheritedFromTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	s, keys, registry := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		// View (0, 0): push PrepareQC for proposal P.
		view0 := types.View{Index: 0, Number: 0}
		pqc0 := makePrepareQC(keys, types.GenProposalForEpoch(rng, registry.MustEpoch(0), view0))
		if err := s.pushPrepareQC(ctx, pqc0); err != nil {
			return fmt.Errorf("pushPrepareQC: %w", err)
		}

		// Timeout at (0, 0) — all votes carry pqc0.
		tqc0 := makeTimeoutQC(keys, view0, utils.Some(pqc0))
		if err := s.pushTimeoutQC(ctx, tqc0); err != nil {
			return fmt.Errorf("pushTimeoutQC(tqc0): %w", err)
		}

		// Now at (0, 1). PrepareQC was cleared; voteTimeout must inherit it.
		view1 := types.View{Index: 0, Number: 1}
		if err := s.voteTimeout(ctx, view1); err != nil {
			return fmt.Errorf("voteTimeout(view1): %w", err)
		}
		tv1, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present at view1")
		}
		if !testTimeoutVotePrepareQC(tv1).IsPresent() {
			return fmt.Errorf("PrepareQC must be inherited from TimeoutQC")
		}

		// Chain through a second timeout to prove it propagates indefinitely.
		tqc1 := makeTimeoutQC(keys, view1, testTimeoutVotePrepareQC(tv1))
		if err := s.pushTimeoutQC(ctx, tqc1); err != nil {
			return fmt.Errorf("pushTimeoutQC(tqc1): %w", err)
		}
		view2 := types.View{Index: 0, Number: 2}
		if err := s.voteTimeout(ctx, view2); err != nil {
			return fmt.Errorf("voteTimeout(view2): %w", err)
		}
		tv2, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present at view2")
		}
		if !testTimeoutVotePrepareQC(tv2).IsPresent() {
			return fmt.Errorf("PrepareQC must survive a third consecutive timeout")
		}
		return nil
	})
	require.NoError(t, err)
}

// TestVoteTimeoutPrepareQC_CurrentViewHigherThanInherited verifies that when
// a reproposal succeeds (PrepareQC forms at the current view), the current
// view's PrepareQC is preferred over the older inherited one.
func TestVoteTimeoutPrepareQC_CurrentViewHigherThanInherited(t *testing.T) {
	rng := utils.TestRng()
	s, keys, registry := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		// View (0, 0): PrepareQC for P.
		view0 := types.View{Index: 0, Number: 0}
		pqc0 := makePrepareQC(keys, types.GenProposalForEpoch(rng, registry.MustEpoch(0), view0))
		if err := s.pushPrepareQC(ctx, pqc0); err != nil {
			return fmt.Errorf("pushPrepareQC(pqc0): %w", err)
		}

		// Timeout at (0, 0) → advance to (0, 1).
		tqc0 := makeTimeoutQC(keys, view0, utils.Some(pqc0))
		if err := s.pushTimeoutQC(ctx, tqc0); err != nil {
			return fmt.Errorf("pushTimeoutQC: %w", err)
		}

		// Reproposal at (0, 1) succeeds — new PrepareQC at view (0, 1).
		view1 := types.View{Index: 0, Number: 1}
		pqc1 := makePrepareQC(keys, types.GenProposalForEpoch(rng, registry.MustEpoch(0), view1))
		if err := s.pushPrepareQC(ctx, pqc1); err != nil {
			return fmt.Errorf("pushPrepareQC(pqc1): %w", err)
		}

		if err := s.voteTimeout(ctx, view1); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present")
		}
		gotPQC := testTimeoutVotePrepareQC(tv)
		if !gotPQC.IsPresent() {
			return fmt.Errorf("PrepareQC should be present")
		}
		pqc, _ := gotPQC.Get()
		if pqc.Proposal().View() != view1 {
			return fmt.Errorf("expected PrepareQC at view %v, got %v", view1, pqc.Proposal().View())
		}
		return nil
	})
	require.NoError(t, err)
}

// TestVoteTimeoutPrepareQC_CurrentViewPresentInheritedNone verifies that when
// the TimeoutQC has no PrepareQC but a fresh one forms in the current view,
// the current view's PrepareQC is used.
func TestVoteTimeoutPrepareQC_CurrentViewPresentInheritedNone(t *testing.T) {
	rng := utils.TestRng()
	s, keys, registry := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		// Timeout at (0, 0) without PrepareQC.
		view0 := types.View{Index: 0, Number: 0}
		tqc0 := makeTimeoutQC(keys, view0, utils.None[*types.PrepareQC]())
		if err := s.pushTimeoutQC(ctx, tqc0); err != nil {
			return fmt.Errorf("pushTimeoutQC: %w", err)
		}

		// Fresh PrepareQC at (0, 1).
		view1 := types.View{Index: 0, Number: 1}
		pqc1 := makePrepareQC(keys, types.GenProposalForEpoch(rng, registry.MustEpoch(0), view1))
		if err := s.pushPrepareQC(ctx, pqc1); err != nil {
			return fmt.Errorf("pushPrepareQC: %w", err)
		}

		if err := s.voteTimeout(ctx, view1); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present")
		}
		gotPQC := testTimeoutVotePrepareQC(tv)
		if !gotPQC.IsPresent() {
			return fmt.Errorf("PrepareQC should be present")
		}
		pqc, _ := gotPQC.Get()
		if pqc.Proposal().View() != view1 {
			return fmt.Errorf("expected PrepareQC at view %v, got %v", view1, pqc.Proposal().View())
		}
		return nil
	})
	require.NoError(t, err)
}

// TestVoteTimeoutPrepareQC_PersistedRestart verifies that after a restart,
// voteTimeout still inherits the PrepareQC from the persisted TimeoutQC.
func TestVoteTimeoutPrepareQC_PersistedRestart(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	dir := t.TempDir()

	makeCfg := func() *Config {
		return &Config{
			Key:                keys[0],
			ViewTimeout:        func(types.View) time.Duration { return time.Hour },
			PersistentStateDir: utils.Some(dir),
		}
	}
	makeDataState := func() *data.State { return newTestDataState(registry) }

	view0 := types.View{Index: 0, Number: 0}
	pqc0 := makePrepareQC(keys, types.GenProposalForEpoch(rng, registry.MustEpoch(0), view0))

	// Session 1: push PrepareQC + TimeoutQC, let runOutputs persist.
	// Hoisted so session 1's WALs can be released after its goroutines have stopped: they hold an
	// exclusive lock on the state directory that session 2 reopens.
	var session1 *State
	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		s, err := NewState(makeCfg(), makeDataState())
		if err != nil {
			return fmt.Errorf("NewState: %w", err)
		}
		session1 = s
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		if err := s.pushPrepareQC(ctx, pqc0); err != nil {
			return fmt.Errorf("pushPrepareQC: %w", err)
		}
		tqc0 := makeTimeoutQC(keys, view0, utils.Some(pqc0))
		if err := s.pushTimeoutQC(ctx, tqc0); err != nil {
			return fmt.Errorf("pushTimeoutQC: %w", err)
		}
		// Wait until runOutputs has processed the state change (and persisted it).
		if _, err := s.myView.Wait(ctx, func(vs types.ViewSpec) bool {
			return vs.TimeoutQC.IsPresent()
		}); err != nil {
			return fmt.Errorf("wait for persist: %w", err)
		}
		return nil
	})
	require.NoError(t, err)

	// scope.Run has stopped session 1's goroutines, so its WALs can be released.
	require.NoError(t, session1.Close())

	// Session 2: restart from persisted state, verify PrepareQC inheritance.
	err = scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		s2, err := NewState(makeCfg(), makeDataState())
		if err != nil {
			return fmt.Errorf("NewState (restart): %w", err)
		}
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s2.Run(ctx)) })

		view1 := types.View{Index: 0, Number: 1}
		if err := s2.voteTimeout(ctx, view1); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s2.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present after restart")
		}
		if !testTimeoutVotePrepareQC(tv).IsPresent() {
			return fmt.Errorf("PrepareQC must be inherited from persisted TimeoutQC after restart")
		}
		return nil
	})
	require.NoError(t, err)
}

func TestPushTimeoutQC_CountsLeaderTimeout(t *testing.T) {
	rng := utils.TestRng()
	s, keys, registry := newTestState(rng)
	view := types.View{Index: 0, Number: 0}
	leader := registry.MustEpoch(0).Committee().Leader(view).ED25519().Address().String()
	timeouts := gathered(t, "tendermint_internal_autobahn_consensus_timeouts", map[string]string{"leader": leader})

	require.NoError(t, s.PushTimeoutQC(t.Context(), makeTimeoutQC(keys, view, utils.None[*types.PrepareQC]())))
	require.Equal(t, timeouts+1, gathered(t, "tendermint_internal_autobahn_consensus_timeouts", map[string]string{"leader": leader}))

	require.NoError(t, s.PushTimeoutQC(t.Context(), makeTimeoutQC(keys, view, utils.None[*types.PrepareQC]())))
	require.Equal(t, timeouts+1, gathered(t, "tendermint_internal_autobahn_consensus_timeouts", map[string]string{"leader": leader}))

	next := view
	next.Number++
	nextLeader := registry.MustEpoch(0).Committee().Leader(next).ED25519().Address().String()
	nextTimeouts := gathered(t, "tendermint_internal_autobahn_consensus_timeouts", map[string]string{"leader": nextLeader})
	require.NoError(t, s.PushTimeoutQC(t.Context(), makeTimeoutQC(keys, next, utils.None[*types.PrepareQC]())))
	require.Equal(t, nextTimeouts+1, gathered(t, "tendermint_internal_autobahn_consensus_timeouts", map[string]string{"leader": nextLeader}))

	require.NoError(t, s.PushTimeoutQC(t.Context(), makeTimeoutQC(keys, view, utils.None[*types.PrepareQC]())))
	require.Equal(t, timeouts+1, gathered(t, "tendermint_internal_autobahn_consensus_timeouts", map[string]string{"leader": leader}))
	require.Equal(t, nextTimeouts+1, gathered(t, "tendermint_internal_autobahn_consensus_timeouts", map[string]string{"leader": nextLeader}))
}

func TestVoteTimeout_RecordsPhases(t *testing.T) {
	rng := utils.TestRng()
	view := types.View{Index: 0, Number: 0}
	phaseCount := func(t *testing.T, leader, phase string) int64 {
		t.Helper()
		return gathered(t, "tendermint_internal_autobahn_consensus_timeout_votes", map[string]string{"leader": leader, "phase": phase})
	}

	s, keys, registry := newTestState(rng)
	leader := registry.MustEpoch(0).Committee().Leader(view).ED25519().Address().String()
	proposal := types.GenProposalForEpoch(rng, registry.MustEpoch(0), view)

	noProposal := phaseCount(t, leader, "no_proposal")
	require.NoError(t, s.voteTimeout(t.Context(), view))
	require.Equal(t, noProposal+1, phaseCount(t, leader, "no_proposal"))
	require.NoError(t, s.voteTimeout(t.Context(), view))
	require.Equal(t, noProposal+1, phaseCount(t, leader, "no_proposal"))

	s, keys, registry = newTestState(rng)
	leader = registry.MustEpoch(0).Committee().Leader(view).ED25519().Address().String()
	for isend := range s.inner.Lock() {
		i := isend.Load()
		i.PrepareVote = utils.Some(types.Sign(keys[0], types.NewPrepareVote(proposal)))
		isend.Store(i)
	}
	noPrepareQC := phaseCount(t, leader, "no_prepare_qc")
	require.NoError(t, s.voteTimeout(t.Context(), view))
	require.Equal(t, noPrepareQC+1, phaseCount(t, leader, "no_prepare_qc"))

	s, keys, registry = newTestState(rng)
	leader = registry.MustEpoch(0).Committee().Leader(view).ED25519().Address().String()
	require.NoError(t, s.pushPrepareQC(t.Context(), makePrepareQC(keys, types.GenProposalForEpoch(rng, registry.MustEpoch(0), view))))
	noCommit := phaseCount(t, leader, "no_commit")
	require.NoError(t, s.voteTimeout(t.Context(), view))
	require.Equal(t, noCommit+1, phaseCount(t, leader, "no_commit"))
}

// gathered reads one series from the default registry. A missing label set is 0.
func gathered(t *testing.T, name string, labels map[string]string) int64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			if len(got) != len(labels) {
				continue
			}
			match := true
			for k, v := range labels {
				if got[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			if c := m.GetCounter(); c != nil {
				return int64(c.GetValue())
			}
			return int64(m.GetGauge().GetValue())
		}
		return 0
	}
	return 0
}

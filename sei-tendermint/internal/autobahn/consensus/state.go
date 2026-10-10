package consensus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

var meters = metrics.Get()

// ViewTimeoutFunc is a function that specifies the timeout for the given view.
// - constant for production
// - custom for tests.
type ViewTimeoutFunc = func(types.View) time.Duration

// Config holds the configuration for the consensus state.
type Config struct {
	Key         types.SecretKey
	ViewTimeout ViewTimeoutFunc
	// ProposalTimeout bounds proposal timestamps and must be greater than 0.
	// The accepted width is ProposalTimeout * (view number + 1).
	ProposalTimeout time.Duration
	// PersistentStateDir is the directory where the consensus state is persisted.
	// If None, persistence is disabled - DANGEROUS, may lead to SLASHING on restart.
	PersistentStateDir utils.Option[string]
}

// State represents the high-level Consensus Control Plane.
// It is responsible for:
// - View management: tracking rounds and leader election.
// - Voting: aggregating signatures for Prepare, Commit, and Timeout phases.
// - Proposals: constructing and verifying block proposals.
//
// NOTE: While this is the "brain", it relies on the "avail" package as its
// primary data store and synchronization sequencer.
type State struct {
	cfg   *Config
	avail *avail.State
	// metrics *Metrics
	inner     utils.Mutex[*utils.AtomicSend[inner]]
	innerRecv utils.AtomicRecv[inner]

	// persister writes inner's persistedInner to disk when PersistentStateDir is set; None when disabled.
	persister utils.Option[persist.Persister[*pb.PersistedInner]]

	timeoutVotes utils.Mutex[*timeoutVotes]
	prepareVotes utils.Mutex[*prepareVotes]
	commitVotes  utils.Mutex[*commitVotes]

	myView        utils.AtomicSend[types.ViewSpec]
	myProposal    utils.AtomicSend[utils.Option[*types.FullProposal]]
	myPrepareVote utils.AtomicSend[utils.Option[*types.ConsensusMsgPrepareVote]]
	myCommitVote  utils.AtomicSend[utils.Option[*types.ConsensusMsgCommitVote]]
	myTimeoutVote utils.AtomicSend[utils.Option[*types.FullTimeoutVote]]
	myTimeoutQC   utils.AtomicSend[utils.Option[*types.TimeoutQC]]
}

// TODO: replace with a single ConsensusMsg stream.
func (s *State) SubscribeProposal() utils.AtomicRecv[utils.Option[*types.FullProposal]] {
	return s.myProposal.Subscribe()
}
func (s *State) SubscribePrepareVote() utils.AtomicRecv[utils.Option[*types.ConsensusMsgPrepareVote]] {
	return s.myPrepareVote.Subscribe()
}
func (s *State) SubscribeCommitVote() utils.AtomicRecv[utils.Option[*types.ConsensusMsgCommitVote]] {
	return s.myCommitVote.Subscribe()
}
func (s *State) SubscribeTimeoutVote() utils.AtomicRecv[utils.Option[*types.FullTimeoutVote]] {
	return s.myTimeoutVote.Subscribe()
}
func (s *State) SubscribeTimeoutQC() utils.AtomicRecv[utils.Option[*types.TimeoutQC]] {
	return s.myTimeoutQC.Subscribe()
}

// NewState constructs a new state.
func NewState(cfg *Config, data *data.State) (*State, error) {
	// Create persister first so newInner can receive the loaded data
	// instead of reading the files directly.
	var pers utils.Option[persist.Persister[*pb.PersistedInner]]
	var persistedData utils.Option[*pb.PersistedInner]
	if dir, ok := cfg.PersistentStateDir.Get(); ok {
		p, d, err := persist.NewPersister[*pb.PersistedInner](utils.Some(dir), innerFile)
		if err != nil {
			return nil, fmt.Errorf("NewPersister: %w", err)
		}
		pers = utils.Some(p)
		persistedData = d
	}
	return newState(cfg, data, pers, persistedData)
}

// newState is the internal constructor exposed for tests that need to inject
// a custom persister (e.g. a failing mock). Production code should use NewState.
func newState(
	cfg *Config,
	data *data.State,
	pers utils.Option[persist.Persister[*pb.PersistedInner]],
	persistedData utils.Option[*pb.PersistedInner],
) (*State, error) {
	if cfg.ProposalTimeout <= 0 {
		return nil, fmt.Errorf("ProposalTimeout must be greater than 0, got %v", cfg.ProposalTimeout)
	}
	availState, err := avail.NewState(cfg.Key, data, cfg.PersistentStateDir)
	if err != nil {
		return nil, fmt.Errorf("avail.NewState: %w", err)
	}

	initialInner, err := newInner(persistedData, availState.SubscribeConsensusSpec().Load(), cfg.Key.Public())
	if err != nil {
		_ = availState.Close()
		return nil, fmt.Errorf("newInner: %w", err)
	}

	innerSend := utils.Alloc(utils.NewAtomicSend(initialInner))
	s := &State{
		cfg: cfg,
		// metrics: NewMetrics(),
		avail:     availState,
		inner:     utils.NewMutex(innerSend),
		innerRecv: innerSend.Subscribe(),
		persister: pers,

		timeoutVotes: utils.NewMutex(newTimeoutVotes()),
		prepareVotes: utils.NewMutex(newPrepareVotes()),
		commitVotes:  utils.NewMutex(newCommitVotes()),

		myView:        utils.NewAtomicSend(types.ViewSpec{ConsensusSpec: initialInner.spec, TimeoutQC: initialInner.TimeoutQC}),
		myProposal:    utils.NewAtomicSend(utils.None[*types.FullProposal]()),
		myPrepareVote: utils.NewAtomicSend(utils.None[*types.ConsensusMsgPrepareVote]()),
		myCommitVote:  utils.NewAtomicSend(utils.None[*types.ConsensusMsgCommitVote]()),
		myTimeoutVote: utils.NewAtomicSend(utils.None[*types.FullTimeoutVote]()),
		myTimeoutQC:   utils.NewAtomicSend(utils.None[*types.TimeoutQC]()),
	}
	view := s.myView.Load()
	meters.ViewNumber.Set(int64(view.View().Number)) // nolint: gosec
	return s, nil
}

// ErrAvailBehindConsensus means the consensus WAL view is ahead of ConsensusSpec.
var ErrAvailBehindConsensus = errors.New("consensus WAL view ahead of ConsensusSpec")

// Close releases the availability state's WALs, and with them the exclusive lock each holds on its
// directory.
//
// Production does not call this: a node exits by rugpull and the OS reclaims everything. It exists so
// that a process which opens the same state directory more than once in its lifetime — a test
// simulating a restart — can release the first State before constructing the second.
func (s *State) Close() error {
	return s.avail.Close()
}

func (s *State) timeoutQC() utils.AtomicRecv[utils.Option[*types.TimeoutQC]] {
	for tv := range s.timeoutVotes.Lock() {
		return tv.qc.Subscribe()
	}
	panic("unreachable")
}

func (s *State) prepareQC() utils.AtomicRecv[utils.Option[*types.PrepareQC]] {
	for pv := range s.prepareVotes.Lock() {
		return pv.qc.Subscribe()
	}
	panic("unreachable")
}

func (s *State) commitQC() utils.AtomicRecv[utils.Option[*types.CommitQC]] {
	for cv := range s.commitVotes.Lock() {
		return cv.qc.Subscribe()
	}
	panic("unreachable")
}

// PushProposal processes an unverified FullProposal message.
func (s *State) PushProposal(ctx context.Context, proposal *types.FullProposal) error {
	return s.pushProposal(ctx, proposal)
}

// PushTimeoutQC processes an unverified TimeoutQC message.
func (s *State) PushTimeoutQC(ctx context.Context, qc *types.TimeoutQC) error {
	return s.pushTimeoutQC(ctx, qc)
}

// verifyVoteEpoch checks that view belongs to ep and key is on ep's committee.
func verifyVoteEpoch(ep *types.Epoch, key types.PublicKey, view types.View) error {
	if err := view.Verify(ep); err != nil {
		return fmt.Errorf("view: %w", err)
	}
	if !ep.Committee().HasReplica(key) {
		return fmt.Errorf("%q is not a replica", key)
	}
	return nil
}

// adoptVoteEpoch drops prepare, commit, and timeout votes collected for any other epoch.
func (s *State) adoptVoteEpoch(epoch types.EpochIndex) {
	for pv := range s.prepareVotes.Lock() {
		pv.adoptEpoch(epoch)
	}
	for cv := range s.commitVotes.Lock() {
		cv.adoptEpoch(epoch)
	}
	for tv := range s.timeoutVotes.Lock() {
		tv.adoptEpoch(epoch)
	}
}

// pushPhaseVote counts vote once myView has reached voteEpoch.
// A vote from an earlier epoch is ignored. A later view in voteEpoch is counted immediately.
func pushPhaseVote[V any, B comparable, QC any](
	ctx context.Context,
	s *State,
	voteEpoch types.EpochIndex,
	votes *utils.Mutex[*phaseVotes[V, B, QC]],
	verify func(*types.Epoch) error,
	vote V,
) error {
	vs, err := s.waitForEpoch(ctx, voteEpoch)
	if err != nil {
		return err
	}
	if vs.Epoch.EpochIndex() != voteEpoch {
		return nil
	}
	if err := verify(vs.Epoch); err != nil {
		return err
	}
	for pv := range votes.Lock() {
		if s.myView.Load().Epoch.EpochIndex() != voteEpoch {
			return nil
		}
		// myView.Store wakes this waiter before adoptVoteEpoch. Adopt under this
		// lock so the previous epoch's votes are dropped before the insert.
		pv.adoptEpoch(voteEpoch)
		pv.pushVerifiedVote(vs.Epoch.Committee(), vote)
	}
	return nil
}

// PushPrepareVote verifies a Prepare vote and counts it once myView has reached the vote's epoch.
// A vote from an earlier epoch is ignored.
// The Prepare vote contains only a proposal; Proposal.Verify runs when the QC is formed.
func (s *State) PushPrepareVote(ctx context.Context, vote *types.Signed[*types.PrepareVote]) error {
	view := vote.Msg().Proposal().View()
	return pushPhaseVote(ctx, s, view.EpochIndex, &s.prepareVotes, func(ep *types.Epoch) error {
		if err := verifyVoteEpoch(ep, vote.Key(), view); err != nil {
			return err
		}
		if err := vote.VerifySig(); err != nil {
			return fmt.Errorf("vote.VerifySig(): %w", err)
		}
		return nil
	}, vote)
}

// PushCommitVote verifies a Commit vote and counts it once myView has reached the vote's epoch.
// A vote from an earlier epoch is ignored.
// The Commit vote contains only a proposal; Proposal.Verify runs when the QC is formed.
func (s *State) PushCommitVote(ctx context.Context, vote *types.Signed[*types.CommitVote]) error {
	view := vote.Msg().Proposal().View()
	return pushPhaseVote(ctx, s, view.EpochIndex, &s.commitVotes, func(ep *types.Epoch) error {
		if err := verifyVoteEpoch(ep, vote.Key(), view); err != nil {
			return err
		}
		if err := vote.VerifySig(); err != nil {
			return fmt.Errorf("vote.VerifySig(): %w", err)
		}
		return nil
	}, vote)
}

// PushTimeoutVote verifies a timeout vote and counts it once myView has reached the vote's epoch.
// A vote from an earlier epoch is ignored.
func (s *State) PushTimeoutVote(ctx context.Context, vote *types.FullTimeoutVote) error {
	return pushPhaseVote(ctx, s, vote.View().EpochIndex, &s.timeoutVotes, func(ep *types.Epoch) error {
		if err := vote.Verify(ep); err != nil {
			return fmt.Errorf("vote.Verify(): %w", err)
		}
		return nil
	}, vote)
}

// Data is the underlying data state.
func (s *State) Data() *data.State   { return s.avail.Data() }
func (s *State) Avail() *avail.State { return s.avail }

// Constructs new proposals.
func (s *State) runPropose(ctx context.Context) error {
	return s.myView.Iter(ctx, func(ctx context.Context, vs types.ViewSpec) error {
		if vs.Epoch.Committee().Leader(vs.View()) != s.cfg.Key.Public() {
			return nil // not the leader.
		}
		// Try repropose.
		if fullProposal, ok := types.NewReproposal(s.cfg.Key, vs); ok {
			s.myProposal.Store(utils.Some(fullProposal))
			return nil
		}
		// Wait for laneQCs.
		laneQCsMap, err := s.avail.WaitForLaneQCs(ctx, vs.Epoch, vs.CommitQC)
		if err != nil {
			return fmt.Errorf("s.avail.WaitForLaneQCs(): %w", err)
		}
		// Construct a full proposal.
		fullProposal, err := types.NewProposal(
			s.cfg.Key,
			vs,
			vs.ClampTimestamp(time.Now(), s.cfg.ProposalTimeout),
			laneQCsMap,
		)
		if err != nil {
			return fmt.Errorf("s.avail.WaitForProposal(): %w", err)
		}
		s.myProposal.Store(utils.Some(fullProposal))
		return nil
	})
}

func updateOutput[T types.ConsensusMsg](w *utils.AtomicSend[utils.Option[T]], v T) {
	old := w.Load()
	if !v.View().Less(types.NextViewOpt(old)) {
		w.Store(utils.Some(v))
	}
}

// Updates the outputs based on the inner state.
// Persists state to disk before broadcasting votes to ensure votes are durable
// before dissemination (prevents double-voting on crash).
// myView update is safe before persist — it only triggers proposing and timeout
// timers, neither of which constitutes a vote. An epoch change adopts that
// epoch on the vote maps.
func (s *State) runOutputs(ctx context.Context) error {
	return s.innerRecv.Iter(ctx, func(ctx context.Context, i inner) error {
		vs := types.ViewSpec{ConsensusSpec: i.spec, TimeoutQC: i.TimeoutQC}
		old := s.myView.Load()
		if old.View().Less(vs.View()) {
			s.myView.Store(vs)
			meters.ViewNumber.Set(int64(vs.View().Number)) // nolint: gosec
			if old.Epoch.EpochIndex() != vs.Epoch.EpochIndex() {
				s.adoptVoteEpoch(vs.Epoch.EpochIndex())
			}
		}
		// Persist to disk before broadcasting votes to the network.
		if p, ok := s.persister.Get(); ok {
			if err := p.Persist(innerProtoConv.Encode(&i.persistedInner)); err != nil {
				return fmt.Errorf("persist inner: %w", err)
			}
		}
		if v, ok := i.PrepareVote.Get(); ok {
			updateOutput(&s.myPrepareVote, &types.ConsensusMsgPrepareVote{Signed: v})
		}
		if v, ok := i.CommitVote.Get(); ok {
			updateOutput(&s.myCommitVote, &types.ConsensusMsgCommitVote{Signed: v})
		}
		if v, ok := i.TimeoutVote.Get(); ok {
			updateOutput(&s.myTimeoutVote, v)
		}
		if v, ok := i.TimeoutQC.Get(); ok {
			updateOutput(&s.myTimeoutQC, v)
		}
		return nil
	})
}

// Run runs the background processes of the consensus state.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("avail", func() error { return s.avail.Run(ctx) })
		scope.SpawnNamed("propose", func() error { return s.runPropose(ctx) })
		scope.SpawnNamed("outputs", func() error { return s.runOutputs(ctx) })
		scope.SpawnNamed("storeCommitQC", func() error {
			return s.commitQC().Iter(ctx, func(ctx context.Context, qc utils.Option[*types.CommitQC]) error {
				if qc, ok := qc.Get(); ok {
					// s.metrics.ObserveCommitQC(qc)
					// We push the locally generated CommitQC into "avail" to act as a
					// sequencer and to trigger data pruning.
					return s.avail.PushCommitQC(ctx, qc)
				}
				return nil
			})
		})
		scope.SpawnNamed("pushSpec", func() error {
			// We pull the tip back from "avail" for dissemination. This ensures we
			// only advance on CommitQCs that avail has verified, persisted, and paired
			// with the epoch of the next RoadIndex — consensus resolves no epochs itself.
			return s.avail.SubscribeConsensusSpec().Iter(ctx, func(ctx context.Context, spec types.ConsensusSpec) error {
				return s.pushSpec(spec)
			})
		})
		scope.SpawnNamed("pushPrepareQC", func() error {
			return s.prepareQC().Iter(ctx, func(ctx context.Context, qc utils.Option[*types.PrepareQC]) error {
				if qc, ok := qc.Get(); ok {
					return s.pushPrepareQC(ctx, qc)
				}
				return nil
			})
		})
		scope.SpawnNamed("pushTimeoutQC", func() error {
			return s.timeoutQC().Iter(ctx, func(ctx context.Context, qc utils.Option[*types.TimeoutQC]) error {
				if qc, ok := qc.Get(); ok {
					return s.pushTimeoutQC(ctx, qc)
				}
				return nil
			})
		})
		scope.SpawnNamed("voteTimeout", func() error {
			nextView := types.View{}
			return s.myView.Iter(ctx, func(ctx context.Context, vs types.ViewSpec) error {
				view := vs.View()
				if view.Less(nextView) {
					return nil
				}
				nextView = view
				if err := utils.Sleep(ctx, s.cfg.ViewTimeout(view)); err != nil {
					return err
				}
				return s.voteTimeout(ctx, view)
			})
		})
		return nil
	})
}

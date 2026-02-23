package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// ViewTimeoutFunc is a function that specifies the timeout for the given view.
// - constant for production
// - custom for tests.
type ViewTimeoutFunc = func(types.View) time.Duration

// Config holds the configuration for the consensus state.
type Config struct {
	Key         types.SecretKey
	ViewTimeout ViewTimeoutFunc
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
	myPrepareVote utils.AtomicSend[utils.Option[*types.ConsensusReqPrepareVote]]
	myCommitVote  utils.AtomicSend[utils.Option[*types.ConsensusReqCommitVote]]
	myTimeoutVote utils.AtomicSend[utils.Option[*types.FullTimeoutVote]]
	myTimeoutQC   utils.AtomicSend[utils.Option[*types.TimeoutQC]]
}

// TODO: replace with a single ConsensusMsg stream.
func (s *State) SubscribeProposal() utils.AtomicRecv[utils.Option[*types.FullProposal]] {
	return s.myProposal.Subscribe()
}
func (s *State) SubscribePrepareVote() utils.AtomicRecv[utils.Option[*types.ConsensusReqPrepareVote]] {
	return s.myPrepareVote.Subscribe()
}
func (s *State) SubscribeCommitVote() utils.AtomicRecv[utils.Option[*types.ConsensusReqCommitVote]] {
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
		p, d, err := persist.NewPersister[*pb.PersistedInner](dir, innerFile)
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
	initialInner, err := newInner(persistedData, data.Committee())
	if err != nil {
		return nil, fmt.Errorf("newInner: %w", err)
	}

	availState, err := avail.NewState(cfg.Key, data, cfg.PersistentStateDir)
	if err != nil {
		return nil, fmt.Errorf("avail.NewState: %w", err)
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

		myView:        utils.NewAtomicSend(types.ViewSpec{}),
		myProposal:    utils.NewAtomicSend(utils.None[*types.FullProposal]()),
		myPrepareVote: utils.NewAtomicSend(utils.None[*types.ConsensusReqPrepareVote]()),
		myCommitVote:  utils.NewAtomicSend(utils.None[*types.ConsensusReqCommitVote]()),
		myTimeoutVote: utils.NewAtomicSend(utils.None[*types.FullTimeoutVote]()),
		myTimeoutQC:   utils.NewAtomicSend(utils.None[*types.TimeoutQC]()),
	}
	return s, nil
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

// WaitForCapacity waits until a new block can be produced by this node.
func (s *State) WaitForCapacity(ctx context.Context) error {
	return s.avail.WaitForCapacity(ctx, s.cfg.Key.Public())
}

// ProduceBlock produces a new block with the given payload.
// Returns ErrNoCapacity if there is currently no capacity for the next block.
// Run WaitForCapacity before calling ProduceBlock.
func (s *State) ProduceBlock(ctx context.Context, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	return s.avail.ProduceBlock(ctx, payload)
}

// PushProposal processes an unverified FullProposal message.
func (s *State) PushProposal(ctx context.Context, proposal *types.FullProposal) error {
	return s.pushProposal(ctx, proposal)
}

// PushTimeoutQC processes an unverified TimeoutQC message.
func (s *State) PushTimeoutQC(ctx context.Context, qc *types.TimeoutQC) error {
	return s.pushTimeoutQC(ctx, qc)
}

// PushPrepareVote processes an unverified Prepare vote message.
func (s *State) PushPrepareVote(vote *types.Signed[*types.PrepareVote]) error {
	if err := vote.VerifySig(s.Data().Committee()); err != nil {
		return fmt.Errorf("vote.VerifySig(): %w", err)
	}
	for pv := range s.prepareVotes.Lock() {
		pv.pushVote(s.Data().Committee(), vote)
	}
	return nil
}

// PushCommitVote processes an unverified CommitVote message.
func (s *State) PushCommitVote(vote *types.Signed[*types.CommitVote]) error {
	if err := vote.VerifySig(s.Data().Committee()); err != nil {
		return fmt.Errorf("vote.VerifySig(): %w", err)
	}
	for cv := range s.commitVotes.Lock() {
		cv.pushVote(s.Data().Committee(), vote)
	}
	return nil
}

// PushTimeoutVote processes an unverified FullTimeoutVote message.
func (s *State) PushTimeoutVote(vote *types.FullTimeoutVote) error {
	if err := vote.Verify(s.Data().Committee()); err != nil {
		return fmt.Errorf("vote.Verify(): %w", err)
	}
	for tv := range s.timeoutVotes.Lock() {
		tv.pushVote(s.Data().Committee(), vote)
	}
	return nil
}

// Data is the underlying data state.
func (s *State) Data() *data.State   { return s.avail.Data() }
func (s *State) Avail() *avail.State { return s.avail }

// Constructs new proposals.
func (s *State) runPropose(ctx context.Context) error {
	committee := s.Data().Committee()
	return s.myView.Iter(ctx, func(ctx context.Context, vs types.ViewSpec) error {
		if committee.Leader(vs.View()) != s.cfg.Key.Public() {
			return nil // not the leader.
		}
		// Try repropose.
		if fullProposal, ok := types.NewReproposal(s.cfg.Key, vs); ok {
			s.myProposal.Store(utils.Some(fullProposal))
			return nil
		}
		// Wait for laneQCs.
		laneQCsMap, err := s.avail.WaitForLaneQCs(ctx, vs.CommitQC)
		if err != nil {
			return fmt.Errorf("s.avail.WaitForLaneQCs(): %w", err)
		}
		// Construct a full proposal.
		fullProposal, err := types.NewProposal(
			s.cfg.Key,
			committee,
			vs,
			time.Now(),
			laneQCsMap,
			s.avail.LastAppQC(),
		)
		if err != nil {
			return fmt.Errorf("s.avail.WaitForProposal(): %w", err)
		}
		s.myProposal.Store(utils.Some(fullProposal))
		return nil
	})
}

func updateOutput[T types.ConsensusReq](w *utils.AtomicSend[utils.Option[T]], v T) {
	old := w.Load()
	if !v.View().Less(types.NextViewOpt(old)) {
		w.Store(utils.Some(v))
	}
}

// Updates the outputs based on the inner state.
// Persists state to disk before broadcasting votes to ensure votes are durable
// before dissemination (prevents double-voting on crash).
// myView update is safe before persist — it only triggers proposing and timeout
// timers, neither of which constitutes a vote.
func (s *State) runOutputs(ctx context.Context) error {
	return s.innerRecv.Iter(ctx, func(ctx context.Context, i inner) error {
		vs := types.ViewSpec{CommitQC: i.CommitQC, TimeoutQC: i.TimeoutQC}
		old := s.myView.Load()
		if old.View().Less(vs.View()) {
			s.myView.Store(vs)
		}
		// Persist to disk before broadcasting votes to the network.
		if p, ok := s.persister.Get(); ok {
			if err := p.Persist(innerProtoConv.Encode(&i.persistedInner)); err != nil {
				return fmt.Errorf("persist inner: %w", err)
			}
		}
		if v, ok := i.PrepareVote.Get(); ok {
			updateOutput(&s.myPrepareVote, &types.ConsensusReqPrepareVote{Signed: v})
		}
		if v, ok := i.CommitVote.Get(); ok {
			updateOutput(&s.myCommitVote, &types.ConsensusReqCommitVote{Signed: v})
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
		scope.SpawnNamed("pushCommitQC", func() error {
			// We pull the CommitQC back from "avail" for dissemination. This ensures
			// that we only push CommitQCs that have been successfully "logged" and
			// sequenced by the availability layer.
			return s.avail.LastCommitQC().Iter(ctx, func(ctx context.Context, last utils.Option[*types.CommitQC]) error {
				if qc, ok := last.Get(); ok {
					return s.pushCommitQC(qc)
				}
				return nil
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

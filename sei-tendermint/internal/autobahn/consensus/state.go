package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/autobahn/consensus/avail"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// ViewTimeoutFunc is a function that specifies the timeout for the given view.
// - constant for production
// - custom for tests.
type ViewTimeoutFunc = func(types.View) time.Duration

// Config holds the configuration for the consensus state.
type Config struct {
	Key         types.SecretKey
	ViewTimeout ViewTimeoutFunc
}

// State represents the current state of the consensus process.
type State struct {
	cfg     *Config
	avail   *avail.State
	metrics *Metrics
	inner   utils.AtomicSend[inner]

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

// NewState constructs a new state.
func NewState(cfg *Config, data *data.State) *State {
	return &State{
		cfg:     cfg,
		metrics: NewMetrics(),
		avail:   avail.NewState(cfg.Key, data),
		inner:   utils.NewAtomicSend(inner{}),

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
func (s *State) ProduceBlock(ctx context.Context, payload *types.Payload) (*types.Block, error) {
	return s.avail.ProduceBlock(ctx, s.cfg.Key.Public(), payload)
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
func (s *State) Data() *data.State {
	return s.avail.Data()
}

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
func (s *State) runOutputs(ctx context.Context) error {
	return s.inner.Iter(ctx, func(ctx context.Context, i inner) error {
		old := s.myView.Load()
		if old.View().Less(i.viewSpec.View()) {
			s.myView.Store(i.viewSpec)
		}
		if v, ok := i.prepareVote.Get(); ok {
			updateOutput(&s.myPrepareVote, &types.ConsensusReqPrepareVote{Signed: v})
		}
		if v, ok := i.commitVote.Get(); ok {
			updateOutput(&s.myCommitVote, &types.ConsensusReqCommitVote{Signed: v})
		}
		if v, ok := i.timeoutVote.Get(); ok {
			updateOutput(&s.myTimeoutVote, v)
		}
		if v, ok := i.viewSpec.TimeoutQC.Get(); ok {
			updateOutput(&s.myTimeoutQC, v)
		}
		return nil
	})
}

// Run runs the background processes of the consensus state.
func (s *State) Run(ctx context.Context) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("avail", func() error { return s.avail.Run(ctx) })
		scope.SpawnNamed("propose", func() error { return s.runPropose(ctx) })
		scope.SpawnNamed("outputs", func() error { return s.runOutputs(ctx) })
		scope.SpawnNamed("storeCommitQC", func() error {
			return s.commitQC().Iter(ctx, func(ctx context.Context, qc utils.Option[*types.CommitQC]) error {
				if qc, ok := qc.Get(); ok {
					s.metrics.ObserveCommitQC(qc)
					return s.avail.PushCommitQC(ctx, qc)
				}
				return nil
			})
		})
		scope.SpawnNamed("pushCommitQC", func() error {
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
	}))
}

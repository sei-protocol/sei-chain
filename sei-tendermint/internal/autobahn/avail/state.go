package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

var ErrBadLane = errors.New("bad lane")

const BlocksPerLane = 3 * types.MaxLaneRangeInProposal

// State represents the Data Availability Plane and Ordered Event Log.
// Although it resides in a sub-package, it serves as the "source of truth" for:
// - Block data: storing and disseminating raw transaction payloads (lanes).
// - Finality tracking: acting as a persistent buffer for CommitQCs and AppQCs.
// - Pruning: managing memory by deleting data once enough execution proofs (AppVotes) are seen.
//
// NOTE: This component is more than an observer; it actively aggregates AppVotes
// to trigger internal pruning, which allows it to manage memory independently
// of the main consensus loop.
type State struct {
	key      types.SecretKey
	data     *data.State
	inner    utils.Watch[*inner]
	epochDuo utils.AtomicRecv[types.EpochDuo] // Load-only view of inner.epoch

	// persisters groups all disk persistence components.
	// Always initialized: real when stateDir is set, no-op otherwise.
	persisters persisters
}

func (s *State) PublicKey() types.PublicKey {
	return s.key.Public()
}

// NewState constructs a new availability state.
// stateDir is None when persistence is disabled (testing only); a no-op
// persist goroutine still runs to bump cursors without disk I/O.
func NewState(key types.SecretKey, data *data.State, stateDir utils.Option[string]) (*State, error) {
	loaded, pers, err := loadPersistedState(stateDir)
	if err != nil {
		return nil, err
	}

	// DuoAt(CommitQC tipcut) happens inside newInner. Seeding is
	// data.SetupInitialDuo; missing epoch hard-fails.
	// Tip order: consensus.NewState requires avail ≥ consensus; avail/consensus
	// may lag data and catch up in Run.
	commitTip := types.RoadIndex(0)
	if ls, ok := loaded.Get(); ok {
		commitTip = ls.nextCommitQC()
	}
	inner, err := newInner(data.Registry(), commitTip, loaded)
	if err != nil {
		return nil, err
	}

	return &State{
		key:        key,
		data:       data,
		inner:      utils.NewWatch(inner),
		epochDuo:   inner.epoch.Subscribe(),
		persisters: pers,
	}, nil
}

func (s *State) FirstCommitQC() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.commits.qcs.first
	}
	panic("unreachable")
}

// NextCommitQC is the next CommitQC road after restore/admit (commits.qcs.next).
func (s *State) NextCommitQC() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.commits.qcs.next
	}
	panic("unreachable")
}

// Data returns the data state.
func (s *State) Data() *data.State {
	return s.data
}

// LastCommitQC returns receiver of the LastCommitQC.
// The tip is the latest durably persisted CommitQC, not merely the admitted tip.
func (s *State) LastCommitQC() utils.AtomicRecv[utils.Option[*types.CommitQC]] {
	for inner := range s.inner.Lock() {
		return inner.commits.persistedCommitQC.Subscribe()
	}
	panic("unreachable")
}

// Run runs the background tasks of the state.
//
// Goroutines: this method spawns long-lived goroutines via scope.SpawnNamed
// (persist, epoch advance, and the FullCommitQC→data-state pusher). Inside
// runPersist, scope.Parallel spawns short-lived goroutines for concurrent
// per-lane block and commit-QC persistence. The persist package itself does
// not spawn goroutines.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("persist", func() error {
			return s.runPersist(ctx, s.persisters)
		})
		scope.SpawnNamed("advanceEpoch", func() error {
			return s.runAdvanceEpoch(ctx)
		})
		// Task inserting FullCommitQCs and local blocks to data state.
		// ErrPruned jumps n forward (AppQC/window prune during catch-up): skipped
		// roads need not be exported locally — peers can PushQC into data.
		scope.SpawnNamed("s.data.PushQC", func() error {
			for n := types.RoadIndex(0); ; n = max(n+1, s.FirstCommitQC()) {
				qc, err := s.fullCommitQC(ctx, n)
				if err != nil {
					if errors.Is(err, types.ErrPruned) {
						continue
					}
					return err
				}

				// Collect locally available blocks for the QC's headers.
				var blocks []*types.Block
				for inner := range s.inner.Lock() {
					for _, h := range qc.Headers() {
						ls, ok := inner.lanes.get(h.Lane())
						if !ok {
							continue
						}
						if b, ok := ls.blocks.q[h.BlockNumber()]; ok {
							// We don't need to check the blocks against the headers,
							// as bad blocks will be filtered out by PushQC anyway.
							blocks = append(blocks, b.Msg().Block())
						}
					}
				}
				if err := s.data.PushQC(ctx, qc, blocks); err != nil {
					return fmt.Errorf("s.data.PushQC(): %w", err)
				}
			}
		})
		return nil
	})
}

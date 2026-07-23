package epoch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "autobahn", "epoch")

// EpochLength is the number of road indices per epoch.
const EpochLength types.RoadIndex = 108_000

// IndexForRoad returns the epoch index containing road.
func IndexForRoad(road types.RoadIndex) types.EpochIndex {
	return types.EpochIndex(road / EpochLength)
}

// FirstRoad returns the first road index of epoch idx.
func FirstRoad(idx types.EpochIndex) types.RoadIndex {
	return types.RoadIndex(idx) * EpochLength
}

// LastRoad returns the last road index of epoch idx (half-open Next-1).
func LastRoad(idx types.EpochIndex) types.RoadIndex {
	return FirstRoad(idx+1) - 1
}

type registryState struct {
	m      map[types.EpochIndex]*types.Epoch
	latest types.EpochIndex
}

// Registry is the authoritative source of epoch and committee information.
// All layers (consensus, data, avail) read from it.
//
// The registry is independent of Availability pruning: Avail keeps a bounded
// Prev|Current operating window, while the registry may retain older (and
// forward-seeded) epochs for restart and admission / execution-leash logic.
// Live admit/export uses each layer's EpochDuo; Registry.EpochAt / WaitForDuo
// are not a substitute for that window.
//
// # Tip interlocking (commit ↔ execution)
//
// Commit and execution are independent pipelines but must stay interlocked:
//
//   - Forward: execution cannot pass commit.
//   - Backward (coarse): consensus must not enter epoch N+1 before execution
//     has finished epoch N-1. Finishing the last road of epoch N-1 registers
//     epoch N+1 (AdvanceIfNeeded: last road of M seeds M+2, so M=N-1 → N+1).
//     Avail does not track per-road exec progress — it only gates when sealing
//     epoch N (last CommitQC of N): N+1 must already exist.
//   - Do not soft-prune, skip roads, or silently repair tip skew.
//
// Avail keeps a two-epoch operating window {E-1, E}. CommitQCs span a suffix of
// E-1 and a prefix of E; AppQC is the prune floor. The window must not drop
// E-1 until E-1 is fully pruned — otherwise FullCommitQC export would skip
// still-queued roads (data/ cannot gap).
//
// Leashes on avail CommitQC insert when sealing epoch N > 0 (last road of N) —
// PushCommitQC and PushAppQC tipcut pushBack (AppQC/CommitQC arrive on separate
// streams). Mid-N admits need only the Current window / committee N:
//
//   - AppQC of N — duo would drop N-1; require N-1 fully pruned first (prune leash).
//   - Registry has epoch N+1 — wait via WaitForDuo (execution leash). Gate on
//     epoch existence, not PushAppHash / local-exec cursors. Epoch 0 has no
//     prior epoch to drop / unlock.
//
// Restart:
//
//   - data/ is the sole restart seeder (SetupInitialDuo from data.NewState).
//     Avail/consensus tips may lead data (async FullCommitQC→data.PushQC), but
//     do not seed the registry. Lead into an unseeded epoch → EpochAt/DuoAt
//     hard-fail (no soft-heal). When lastExecuted is already in the CommitQC
//     tip's epoch N, EnsureAfterExecuted seeds N+1 (N-1 is done) — that is how
//     a tipcut in N+1 stays inside the reconstructed window. Leading by more
//     than one epoch should not happen: sealing N+1 needs registry N+2, which
//     needs execution of LastRoad(N), which needs that CommitQC in data — so if
//     data tip is still in N, avail/consensus cannot have entered N+2. If it
//     did, EpochAt/DuoAt hard-fails (corrupt / inconsistent local state; do
//     not soft-heal).
//   - After construction: consensus checks avail tipcut >= consensus tipcut;
//     the giga validator router checks consensus tipcut >= data.CommitTipCut()
//     (together ⇒ avail >= data). Behind → hard-fail.
//   - SetupInitialDuo may register genesis-committee placeholders (temporary;
//     real committees TBD) — that is not inventing roads or repairing
//     inconsistent tips.
//   - FullCommitQC export: ErrRoadBeforeWindow → data.ErrPruned (caller jumps
//     ahead). Safe because the boundary AppQC-of-N leash ensures before-window
//     roads are already pruned; ErrRoadAfterWindow hard-fails (no wait).
//   - data.PushQC / PushBlock: before-window hard-fails (epochDuo only). Unlike
//     export, ingest must not soft-map to ErrPruned or Registry.EpochAt — the
//     WaitForDuo leash keeps Prev available for catch-up fill; a duo already at
//     {N,N+1} means N-1 is too old to admit (restart soft-heal forbidden).
type Registry struct {
	state utils.RWMutex[*registryState]
	// highestEpoch is a monotonic high-water mark for WaitForDuo.
	// Kept off registryState so EpochAt can stay on the RLock fast path.
	highestEpoch utils.AtomicSend[types.EpochIndex]
}

// NewRegistry creates a Registry with the genesis committee.
// Initial seeding registers epochs {0,1} (both defined by genesis); epoch 2 is
// registered when the last road of epoch 0 is executed (AdvanceIfNeeded).
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	ep := types.NewEpoch(0, types.RoadRange{First: 0, Next: FirstRoad(1)}, genesisTimestamp, committee, firstBlock)
	r := &Registry{
		state: utils.NewRWMutex(&registryState{
			m:      map[types.EpochIndex]*types.Epoch{0: ep},
			latest: 0,
		}),
		highestEpoch: utils.NewAtomicSend(types.EpochIndex(0)),
	}
	// TODO: in the future this information will be read from disk and verified
	// (snapshots / state sync); until then seed a genesis placeholder.
	r.SetupInitialDuo(utils.None[types.RoadIndex](), utils.None[CommitQCSpan]())
	return r, nil
}

// CommitQCSpan is the inclusive first/last CommitQC proposal roads loaded from
// the data WAL. Both ends are always set together (single QC ⇒ First == Last).
type CommitQCSpan struct {
	First, Last types.RoadIndex
}

// SetupInitialDuo seeds placeholder epochs on restart. Called only from
// data.NewState (see Registry tip-interlock docs). Avail/consensus do not seed.
//
//  1. Seed every epoch from commitQCs.First through commitQCs.Last (the loaded
//     WAL span). Stand-in for reading committee/epoch info from retained blocks;
//     today placeholders reuse the genesis committee.
//  2. EnsureDuoAt(commit tipcut) so data/avail DuoAt(Index+1) works — including
//     when the tip closes epoch N (tipcut needs N+1 even if execution lags).
//  3. EnsureAfterExecuted(lastExecuted) restores live AdvanceIfNeeded lookahead.
//
// If execution is past the CommitQC tip, that is a bug — warn and ignore it.
// None/None (fresh start) seeds {0, 1}. Idempotent for existing entries.
//
// TODO(autobahn): lastExecutedRoad is derived from app.LastBlockHeight() (Cosmos
// app state DB). Do not keep depending on the app DB for execution tip / epoch
// seeding — persist that in Giga storage alongside CommitQC/AppQC WALs.
// TODO(autobahn): replace genesis placeholders with epoch info carried on blocks.
func (r *Registry) SetupInitialDuo(lastExecutedRoad utils.Option[types.RoadIndex], commitQCs utils.Option[CommitQCSpan]) {
	var windowFirst, windowLast types.EpochIndex
	haveWindow := false
	executedForAdvance := utils.None[types.RoadIndex]()

	if span, ok := commitQCs.Get(); ok {
		windowFirst = IndexForRoad(span.First)
		windowLast = IndexForRoad(span.Last)
		if windowFirst > windowLast {
			logger.Warn("first CommitQC epoch past tip on restart; clamping to tip",
				slog.Uint64("first_road", uint64(span.First)),
				slog.Uint64("tip_road", uint64(span.Last)))
			windowFirst = windowLast
		}
		haveWindow = true
	}

	if road, ok := lastExecutedRoad.Get(); ok {
		if span, cok := commitQCs.Get(); cok && road > span.Last {
			logger.Warn("execution tip past CommitQC tip on restart; ignoring executed tip for epoch seeding",
				slog.Uint64("executed_road", uint64(road)),
				slog.Uint64("commit_qc_road", uint64(span.Last)))
		} else {
			tipEpoch := IndexForRoad(road)
			if !haveWindow {
				// Execution-only: open Prev|Current around the executed tip.
				windowFirst = 0
				if tipEpoch >= 1 {
					windowFirst = tipEpoch - 1
				}
				windowLast = tipEpoch
				haveWindow = true
			}
			executedForAdvance = utils.Some(road)
		}
	}

	if !haveWindow {
		windowFirst, windowLast = 0, 1 // fresh start
	}

	for s := range r.state.Lock() {
		for idx := windowFirst; idx <= windowLast; idx++ {
			if _, ok := s.m[idx]; ok {
				continue
			}
			_, _ = r.makeEpoch(s, idx) //nolint:errcheck // genesis always present
		}
	}

	if span, ok := commitQCs.Get(); ok {
		r.EnsureDuoAt(span.Last + 1) // operating tipcut after last CommitQC
	}
	if road, ok := executedForAdvance.Get(); ok {
		r.EnsureAfterExecuted(road)
	}
}

// FirstBlock returns the first global block number of the genesis epoch.
// Used as the cold-start default (no WAL, no snapshot); WAL overrides this on restart.
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	for s := range r.state.RLock() {
		return s.m[0].FirstBlock()
	}
	panic("unreachable")
}

// EpochAt returns the epoch for the given road index.
// Returns an error if the epoch has not been registered via SetupInitialDuo or
// AdvanceIfNeeded.
func (r *Registry) EpochAt(roadIndex types.RoadIndex) (*types.Epoch, error) {
	epochIdx := IndexForRoad(roadIndex)
	for s := range r.state.RLock() {
		if ep, ok := s.m[epochIdx]; ok {
			return ep, nil
		}
		return nil, fmt.Errorf("epoch %d (road %d) not registered", epochIdx, roadIndex)
	}
	panic("unreachable")
}

// makeEpoch constructs a new epoch at epochIdx using the genesis committee and
// inserts it into s. Caller must hold the write lock. Overwrites if present;
// callers that must not clobber should check existence first.
// Note: does NOT advance s.latest.
func (r *Registry) makeEpoch(s *registryState, epochIdx types.EpochIndex) (*types.Epoch, error) {
	genesis, ok := s.m[0]
	if !ok {
		return nil, fmt.Errorf("genesis epoch missing from registry")
	}
	firstRoad := FirstRoad(epochIdx)
	epoch := types.NewEpoch(epochIdx, types.RoadRange{First: firstRoad, Next: FirstRoad(epochIdx + 1)}, genesis.FirstTimestamp(), genesis.Committee(), genesis.FirstBlock())
	s.m[epochIdx] = epoch
	// Wake WaitForDuo waiters. makeEpoch runs under the write lock, so this
	// Load/Store is serialized; highestEpoch only advances.
	if epochIdx > r.highestEpoch.Load() {
		r.highestEpoch.Store(epochIdx)
	}
	return epoch, nil
}

// EnsureEpoch registers a genesis-committee placeholder for idx if missing.
func (r *Registry) EnsureEpoch(idx types.EpochIndex) {
	for s := range r.state.RLock() {
		if _, ok := s.m[idx]; ok {
			return
		}
	}
	for s := range r.state.Lock() {
		if _, ok := s.m[idx]; !ok {
			_, _ = r.makeEpoch(s, idx) //nolint:errcheck // genesis always present
		}
	}
}

// EnsureDuoAt ensures epochs needed for DuoAt(road) (Current, and Prev when
// center > 0).
func (r *Registry) EnsureDuoAt(road types.RoadIndex) {
	center := IndexForRoad(road)
	if center > 0 {
		r.EnsureEpoch(center - 1)
	}
	r.EnsureEpoch(center)
}

// EnsureAfterExecuted restores the registry lookahead live AdvanceIfNeeded
// would have produced once road was executed. If execution has reached epoch E
// (same epoch as a CommitQC tip in E), epoch E-1 is done → seed E+1. Closing
// road of E additionally seeds E+2 (AdvanceIfNeeded: finishing M → M+2).
func (r *Registry) EnsureAfterExecuted(road types.RoadIndex) {
	tipEpoch := IndexForRoad(road)
	r.EnsureEpoch(tipEpoch + 1)
	if road == LastRoad(tipEpoch) {
		r.EnsureEpoch(tipEpoch + 2)
	}
}

// AdvanceIfNeeded seeds epoch M+2 when the last road of epoch M is
// execution-complete (design: finishing N-1 registers N+1, i.e. M=N-1 → M+2=N+1).
// Call after the last global of that road is executed. Earlier roads in the
// epoch are a no-op. Restart uses EnsureAfterExecuted / EnsureDuoAt instead of
// replaying this with synthetic LastRoad tips.
// Committee for M+2 is currently the genesis committee.
// TODO: pass the real M+2 committee once execution derives it.
func (r *Registry) AdvanceIfNeeded(roadIndex types.RoadIndex) {
	tipEpoch := IndexForRoad(roadIndex)
	if roadIndex != LastRoad(tipEpoch) {
		return
	}
	r.EnsureEpoch(tipEpoch + 2)
}

// DuoAt returns the EpochDuo centered on the epoch containing roadIndex.
// Current must already be present; returns an error if missing. Prev is absent
// only when Current is epoch 0.
//
// The registry retains epochs indefinitely (no pruning). If pruning is added,
// a missing epoch below the retain window should surface as ErrPruned so
// callers can silently drop rather than Wait forever.
func (r *Registry) DuoAt(roadIndex types.RoadIndex) (types.EpochDuo, error) {
	centerIdx := IndexForRoad(roadIndex)
	current, err := r.EpochAt(FirstRoad(centerIdx))
	if err != nil {
		return types.EpochDuo{}, fmt.Errorf("epoch %d (road %d) not in registry", centerIdx, roadIndex)
	}
	duo := types.EpochDuo{Current: current}
	if centerIdx > 0 {
		if prev, err := r.EpochAt(FirstRoad(centerIdx - 1)); err == nil {
			duo.Prev = utils.Some(prev)
		}
	}
	return duo, nil
}

// WaitForDuo blocks until DuoAt(roadIndex) can succeed (Current registered),
// then returns that duo. Same retention note as DuoAt.
// Must not hold the avail/data inner lock (execution seeds via AdvanceIfNeeded).
func (r *Registry) WaitForDuo(ctx context.Context, roadIndex types.RoadIndex) (types.EpochDuo, error) {
	if duo, err := r.DuoAt(roadIndex); err == nil {
		return duo, nil
	}
	centerIdx := IndexForRoad(roadIndex)
	if _, err := r.highestEpoch.Subscribe().Wait(ctx, func(highest types.EpochIndex) bool {
		return highest >= centerIdx
	}); err != nil {
		return types.EpochDuo{}, err
	}
	return r.DuoAt(roadIndex)
}

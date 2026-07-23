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
//     NewRegistry only installs epoch 0; empty BlockDB passes None commit span
//     (genesis only). Avail/consensus tips may lead data (async FullCommitQC→
//     data.PushQC), but do not seed the registry. Lead into an unseeded epoch →
//     EpochAt/DuoAt hard-fail (no soft-heal). When the execution tipcut is already
//     in the CommitQC tip's epoch N, EnsureExecTipcut seeds N+1 (N-1 is done) — that
//     is how a tipcut in N+1 stays inside the reconstructed window. Seeding N+2 still
//     requires the execution tipcut to be past LastRoad(N) (fully finished that
//     road → tipcut LastRoad(N)+1); AdvanceIfNeeded owns the LastRoad check.
//     Leading by more than one epoch should not happen: sealing N+1 needs registry N+2, which
//     needs execution of LastRoad(N), which needs that CommitQC in data — so if
//     data tip is still in N, avail/consensus cannot have entered N+2. If it
//     did, EpochAt/DuoAt hard-fails (corrupt / inconsistent local state; do
//     not soft-heal).
//   - After construction: consensus checks avail tipcut >= consensus tipcut;
//     the giga validator router checks consensus tipcut >= data.CommitTipCut()
//     (together ⇒ avail >= data). Behind → hard-fail.
//   - SetupInitialDuo may register genesis-committee placeholders (temporary;
//     real committees TBD) — that is not inventing roads or repairing
//     inconsistent tips. data.NewState errors if a positive LastExecutedBlock
//     has no covering CommitQC. Empty CommitQC ranges are rejected.
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

// NewRegistry creates a Registry with the genesis committee (epoch 0 only).
// Epoch 1+ placeholders are seeded by data.NewState via SetupInitialDuo.
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	ep := types.NewEpoch(0, types.RoadRange{First: 0, Next: FirstRoad(1)}, genesisTimestamp, committee, firstBlock)
	return &Registry{
		state: utils.NewRWMutex(&registryState{
			m:      map[types.EpochIndex]*types.Epoch{0: ep},
			latest: 0,
		}),
		highestEpoch: utils.NewAtomicSend(types.EpochIndex(0)),
	}, nil
}

// SetupInitialDuo seeds placeholder epochs on restart. Called only from
// data.NewState (see Registry tip-interlock docs). Avail/consensus do not seed.
//
//  1. commitQCs — half-open retained CommitQC road range [First, Next). Seeds
//     every epoch covering [First, Next), then EnsureDuoAt(Next). None = empty
//     store (genesis epoch 0 only). Empty range (First >= Next) panics.
//  2. nextRoadToExecute — half-open execution tipcut; EnsureExecTipcut
//     restores AdvanceIfNeeded lookahead. None = nothing fully executed yet.
//     Ignored when past commit tipcut (Next). Present without a commit span
//     panics (inconsistent: execution cannot lead an empty CommitQC store).
//
// Idempotent for existing entries.
//
// TODO(autobahn): nextRoadToExecute is derived from app.LastBlockHeight() (Cosmos
// app state DB). Do not keep depending on the app DB for execution tip / epoch
// seeding — persist that in Giga storage alongside CommitQC/AppQC.
// TODO(autobahn): replace genesis placeholders with epoch info carried on blocks.
func (r *Registry) SetupInitialDuo(
	nextRoadToExecute utils.Option[types.RoadIndex],
	commitQCs utils.Option[types.RoadRange],
) {
	if span, ok := commitQCs.Get(); ok {
		if span.First >= span.Next {
			panic(fmt.Sprintf("SetupInitialDuo: empty CommitQC range [%d, %d)", span.First, span.Next))
		}
		windowFirst := IndexForRoad(span.First)
		windowLast := IndexForRoad(span.Next - 1)

		for s := range r.state.Lock() {
			for idx := windowFirst; idx <= windowLast; idx++ {
				if _, ok := s.m[idx]; ok {
					continue
				}
				r.makeEpoch(s, idx)
			}
		}
		r.EnsureDuoAt(span.Next)

		if next, ok := nextRoadToExecute.Get(); ok {
			if next > span.Next {
				logger.Warn("execution tipcut past CommitQC tipcut on restart; ignoring for epoch seeding",
					slog.Uint64("execution_tipcut", uint64(next)),
					slog.Uint64("commit_qc_tipcut", uint64(span.Next)))
			} else {
				r.EnsureExecTipcut(next)
			}
		}
		return
	}

	if nextRoadToExecute.IsPresent() {
		panic("execution tipcut without CommitQC span on restart")
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
// Note: does NOT advance s.latest. Panics if genesis (epoch 0) is missing —
// NewRegistry always installs it.
func (r *Registry) makeEpoch(s *registryState, epochIdx types.EpochIndex) *types.Epoch {
	genesis, ok := s.m[0]
	if !ok {
		panic("genesis epoch missing from registry")
	}
	firstRoad := FirstRoad(epochIdx)
	epoch := types.NewEpoch(epochIdx, types.RoadRange{First: firstRoad, Next: FirstRoad(epochIdx + 1)}, genesis.FirstTimestamp(), genesis.Committee(), genesis.FirstBlock())
	s.m[epochIdx] = epoch
	// Wake WaitForDuo waiters. makeEpoch runs under the write lock, so this
	// Load/Store is serialized; highestEpoch only advances.
	if epochIdx > r.highestEpoch.Load() {
		r.highestEpoch.Store(epochIdx)
	}
	return epoch
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
			r.makeEpoch(s, idx)
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

// EnsureExecTipcut restores registry lookahead for a half-open execution tipcut
// (next road that still needs execution). Roads strictly below next are fully
// done — same shape as a CommitQC tipcut. next==0 means nothing completed yet.
// Last completed road is next-1; AdvanceIfNeeded owns whether that road is
// LastRoad(epoch).
func (r *Registry) EnsureExecTipcut(next types.RoadIndex) {
	if next == 0 {
		return
	}
	lastDone := next - 1
	r.EnsureEpoch(IndexForRoad(lastDone) + 1)
	r.AdvanceIfNeeded(lastDone)
}

// AdvanceIfNeeded seeds epoch M+2 when roadIndex is LastRoad(M) (design: finishing
// N-1 registers N+1, i.e. M=N-1 → M+2=N+1). Call only after the last global of
// that road has executed (GlobalRange.IsLastBlock) — the live path and data's
// execution tipcut (road+1 when complete) share that gate; this function owns
// the LastRoad(epoch) check. Earlier roads in the epoch are a no-op.
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

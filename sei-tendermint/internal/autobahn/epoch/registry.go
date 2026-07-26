package epoch

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

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
	m map[types.EpochIndex]*types.Epoch
}

// Registry is the authoritative store of epoch/committee metadata for all
// layers (consensus, data, avail).
//
// Invariants:
//   - Independent of each layer's live EpochDuo (Prev|Current). Duo admits
//     traffic; the registry may retain more epochs for restart and leashes.
//   - Execution cannot pass commit. Sealing epoch N (including 0) requires
//     registry N+1 (execution leash) and AppQC in epoch N before the window
//     slides (prune leash). Epoch 0 is intentionally not exempt: even though
//     {∅,0}→{0,1} drops no Prev, the leash is what guarantees an AppQC anchor
//     before Current leaves 0 (newInner hard-fails without one). Peer
//     PushCommitQC can seal LastRoad(0) without local BlocksPerLane pressure,
//     so "unreachable under BlocksPerLane" is not a valid exemption. Finishing
//     LastRoad(N-1) seeds epoch N+1 (AdvanceIfNeeded).
//   - data/ is the sole restart seeder (SetupInitialDuo). Avail/consensus must
//     not seed; tip into an unseeded epoch → EpochAt/DuoAt hard-fail.
//   - Post-construction tipcuts: avail ≥ consensus. Consensus and avail may
//     lag data (peer FullCommitQC / async BlockDB flush); catch-up from peers
//     and avail LastCommitQC in Run closes the gap.
//   - Placeholders use the genesis committee until real committees are wired.
//
// TODO(autobahn): replace genesis placeholders with epoch info on blocks.
type Registry struct {
	state utils.RWMutex[*registryState]
	// epochGen bumps on every new registration; WaitForDuo waits on it so
	// filling a gap still wakes waiters.
	epochGen utils.AtomicSend[uint64]
}

// NewRegistry creates a Registry with genesis epoch 0 only.
// Epoch 1+ are seeded by data.NewState via SetupInitialDuo.
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	ep := types.NewEpoch(0, types.RoadRange{First: 0, Next: FirstRoad(1)}, genesisTimestamp, committee, firstBlock)
	return &Registry{
		state: utils.NewRWMutex(&registryState{
			m: map[types.EpochIndex]*types.Epoch{0: ep},
		}),
		epochGen: utils.NewAtomicSend(uint64(0)),
	}, nil
}

// SetupInitialDuo seeds placeholder epochs on restart. Call only from
// data.NewState. Idempotent for existing entries.
//
// commitQCs is the half-open retained CommitQC range [First, Next). Seeds every
// epoch covering [First, Next), EnsureDuoAt(Next), then placeholder
// windowLast+1/+2 (see below). None = empty store → EnsureDuoAt(FirstRoad(1))
// so {0,1}. Empty range (First >= Next) returns an error.
func (r *Registry) SetupInitialDuo(commitQCs utils.Option[types.RoadRange]) error {
	if span, ok := commitQCs.Get(); ok {
		if span.First >= span.Next {
			return fmt.Errorf("SetupInitialDuo: empty CommitQC range [%d, %d)", span.First, span.Next)
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
		// Placeholder +1/+2: simplification while committees are genesis stubs
		// (unchanged by exec). Covers exec tip ahead of persisted CommitQC (N+1)
		// and tip at LastRoad(N) without re-exec (N+2). Goes away next PR when
		// committees are linked to execution.
		r.EnsureEpoch(windowLast + 1)
		r.EnsureEpoch(windowLast + 2)
		return nil
	}

	r.EnsureDuoAt(FirstRoad(1))
	return nil
}

// FirstBlock returns the first global block number of the genesis epoch.
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	for s := range r.state.RLock() {
		return s.m[0].FirstBlock()
	}
	panic("unreachable")
}

// EpochAt returns the epoch containing roadIndex.
// Error if that epoch is not registered.
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

// makeEpoch inserts a genesis-committee placeholder at epochIdx.
// Caller holds the write lock. Overwrites if present. Panics without epoch 0.
func (r *Registry) makeEpoch(s *registryState, epochIdx types.EpochIndex) *types.Epoch {
	genesis, ok := s.m[0]
	if !ok {
		panic("genesis epoch missing from registry")
	}
	firstRoad := FirstRoad(epochIdx)
	epoch := types.NewEpoch(epochIdx, types.RoadRange{First: firstRoad, Next: FirstRoad(epochIdx + 1)}, genesis.FirstTimestamp(), genesis.Committee(), genesis.FirstBlock())
	s.m[epochIdx] = epoch
	r.epochGen.Store(r.epochGen.Load() + 1)
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

// EnsureDuoAt ensures epochs for DuoAt(road): Current, and Prev when center > 0.
func (r *Registry) EnsureDuoAt(road types.RoadIndex) {
	center := IndexForRoad(road)
	if center > 0 {
		r.EnsureEpoch(center - 1)
	}
	r.EnsureEpoch(center)
}

// AdvanceIfNeeded seeds epoch M+2 when roadIndex is LastRoad(M); else no-op.
// Also ensures M+1 so WaitForDuo(FirstRoad(M+2)) is not stuck on a Prev gap.
// Call only after the last global of that road has executed (IsLastBlock).
//
// TODO(autobahn): pass the real M+2 committee once execution derives it.
// Until then placeholder committees may seed ahead of real execute results
// (including restart when app Commit leads blockDB flush).
func (r *Registry) AdvanceIfNeeded(roadIndex types.RoadIndex) {
	tipEpoch := IndexForRoad(roadIndex)
	if roadIndex != LastRoad(tipEpoch) {
		return
	}
	r.EnsureEpoch(tipEpoch + 1)
	r.EnsureEpoch(tipEpoch + 2)
}

// DuoAt returns the EpochDuo centered on the epoch containing roadIndex.
// Current must already be registered. Prev absent only for epoch 0; missing
// Prev for center > 0 is a hard error (no soft-degrade to Current-only).
func (r *Registry) DuoAt(roadIndex types.RoadIndex) (types.EpochDuo, error) {
	centerIdx := IndexForRoad(roadIndex)
	current, err := r.EpochAt(FirstRoad(centerIdx))
	if err != nil {
		return types.EpochDuo{}, fmt.Errorf("epoch %d (road %d) not in registry", centerIdx, roadIndex)
	}
	prev := utils.None[*types.Epoch]()
	if centerIdx > 0 {
		p, err := r.EpochAt(FirstRoad(centerIdx - 1))
		if err != nil {
			return types.EpochDuo{}, fmt.Errorf("epoch %d prev (road %d) not in registry", centerIdx-1, roadIndex)
		}
		prev = utils.Some(p)
	}
	return types.NewEpochDuo(current, prev), nil
}

// WaitForDuo blocks until DuoAt(roadIndex) succeeds.
// Waits on epochGen (any registration), so filling Prev after Current is
// already present still unblocks. Must not hold the avail/data inner lock
// (execution may seed via AdvanceIfNeeded).
func (r *Registry) WaitForDuo(ctx context.Context, roadIndex types.RoadIndex) (types.EpochDuo, error) {
	sub := r.epochGen.Subscribe()
	for {
		// Capture gen before DuoAt so a registration between check and Wait still wakes.
		seen := sub.Load()
		if duo, err := r.DuoAt(roadIndex); err == nil {
			return duo, nil
		}
		if _, err := sub.Wait(ctx, func(gen uint64) bool { return gen > seen }); err != nil {
			return types.EpochDuo{}, err
		}
	}
}

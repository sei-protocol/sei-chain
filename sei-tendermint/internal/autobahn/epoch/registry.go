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

type registryState = map[types.EpochIndex]*types.Epoch

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
//   - Genesis FirstBlock / FirstTimestamp live on Registry (from GenDoc), not
//     on Epoch — per-epoch floors come from CommitQCs, which the registry does
//     not store.
//
// TODO(autobahn): replace genesis placeholders with epoch info on blocks.
type Registry struct {
	state utils.Watch[registryState]
	// Genesis floors from GenDoc (InitialHeight / GenesisTime).
	genesisFirstBlock types.GlobalBlockNumber
	genesisTimestamp  time.Time
	genesisCommittee  *types.Committee
}

// NewRegistry creates a Registry with genesis epoch 0 only.
// Epoch 1+ are seeded by data.NewState via SetupInitialDuo.
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	ep := types.NewEpoch(0, types.RoadRange{First: 0, Next: FirstRoad(1)}, committee)
	return &Registry{
		state:             utils.NewWatch(registryState{0: ep}),
		genesisFirstBlock: firstBlock,
		genesisTimestamp:  genesisTimestamp,
		genesisCommittee:  committee,
	}, nil
}

// SetupInitialDuo seeds placeholder epochs on restart. Call only from
// data.NewState. Idempotent for existing entries.
//
// commitQCs is the half-open retained CommitQC range [First, Next). Seeds the
// duo at First, every epoch covering [First, Next), the duo at Next, then
// placeholder windowLast+1/+2 (see below). None = empty store → {0,1};
// execution seeds epoch 2 before sealing epoch 0. Empty range (First >= Next)
// returns an error.
func (r *Registry) SetupInitialDuo(commitQCs utils.Option[types.RoadRange]) error {
	if span, ok := commitQCs.Get(); ok {
		if span.First >= span.Next {
			return fmt.Errorf("SetupInitialDuo: empty CommitQC range [%d, %d)", span.First, span.Next)
		}
		windowFirst := IndexForRoad(span.First)
		windowLast := IndexForRoad(span.Next - 1)

		// Avail WAL and BlockDB prune independently. Avail may restart at the
		// retained span's first epoch and still need its Prev.
		r.EnsureDuoAt(span.First)
		for s,ctrl := range r.state.Lock() {
			for idx := windowFirst; idx <= windowLast; idx++ {
				if _, ok := s[idx]; ok {
					continue
				}
				r.makeEpoch(s, idx)
				ctrl.Updated()
			}
		}
		r.EnsureDuoAt(span.Next)
		// TODO(autobahn-placeholder-seed): always seed windowLast+1/+2 with
		// genesis-committee stubs. Needed today because exec tip can sit ahead
		// of persisted CommitQC (N+1) and tip at LastRoad(N) may need N+2
		// without re-exec. Drop once real committees are linked to execution
		// and seed tipcut+1 only.
		r.EnsureEpoch(windowLast + 1)
		r.EnsureEpoch(windowLast + 2)
		return nil
	}

	r.EnsureDuoAt(FirstRoad(1))
	return nil
}

// FirstBlock returns the first global block number of the genesis epoch.
// Used as the cold-start default (no WAL, no snapshot); WAL overrides this on restart.
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	return r.genesisFirstBlock
}

// FirstTimestamp returns the genesis timestamp (GenDoc.GenesisTime).
func (r *Registry) FirstTimestamp() time.Time {
	return r.genesisTimestamp
}

// EpochAt returns the epoch containing roadIndex.
// Error if that epoch is not registered.
func (r *Registry) EpochAt(roadIndex types.RoadIndex) (*types.Epoch, bool) {
	for s := range r.state.Lock() {
		if ep, ok := s[IndexForRoad(roadIndex)]; ok {
			return ep, true
		}
	}
	return nil, false 
}

// makeEpoch inserts a genesis-committee placeholder at epochIdx.
// Caller holds the write lock. Overwrites if present. Panics without epoch 0.
func (r *Registry) makeEpoch(s registryState, epochIdx types.EpochIndex) *types.Epoch {
	if _, ok := s[0]; !ok {
		panic("genesis epoch missing from registry")
	}
	firstRoad := FirstRoad(epochIdx)
	epoch := types.NewEpoch(epochIdx, types.RoadRange{First: firstRoad, Next: FirstRoad(epochIdx + 1)}, r.genesisCommittee)
	s[epochIdx] = epoch
	return epoch
}

// EnsureEpoch registers a genesis-committee placeholder for idx if missing.
func (r *Registry) EnsureEpoch(idx types.EpochIndex) {
	for s,ctrl := range r.state.Lock() {
		if _, ok := s[idx]; !ok {
			r.makeEpoch(s, idx)
			ctrl.Updated()
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
func (r *Registry) DuoAt(roadIndex types.RoadIndex) (types.EpochDuo, bool) {
	current, ok := r.EpochAt(roadIndex)
	if !ok { return types.EpochDuo{},false }
	prev := utils.None[*types.Epoch]()
	if current.EpochIndex() > 0 {
		p,_ := r.EpochAt(current.RoadRange().First-1)
		prev = utils.Some(p)
	}
	return utils.OrPanic1(types.NewEpochDuo(current, prev)), true
}

// WaitForDuo blocks until DuoAt(roadIndex) succeeds.
// Waits on epochGen (any registration), so filling Prev after Current is
// already present still unblocks. Must not hold the avail/data inner lock
// (execution may seed via AdvanceIfNeeded).
func (r *Registry) WaitForDuo(ctx context.Context, i types.EpochIndex) (types.EpochDuo, error) {
	current,err := r.WaitForEpoch(ctx,i)
	if err!=nil { return types.EpochDuo{},nil }
	prev := utils.None[*types.Epoch]()
	if i>0 {
		p,err := r.WaitForEpoch(ctx,i-1)
		if err!=nil { return types.EpochDuo{},nil }
		prev = utils.Some(p)
	}
	return types.NewEpochDuo(current,prev)
}

func (r *Registry) ConsensusSpec(prev utils.Option[*types.CommitQC]) (*types.ConsensusSpec, bool) {
	duo,ok := r.DuoAt(types.NextIndexOpt(prev))
	if !ok { return nil,false }
	return &types.ConsensusSpec {
		Epochs: duo,
		CommitQC: prev,
		GenesisFirstBlock: r.genesisFirstBlock,
		GenesisTimestamp: r.genesisTimestamp,
	},true
}

func (r *Registry) WaitForConsensusSpec(ctx context.Context, prev utils.Option[*types.CommitQC]) (*types.ConsensusSpec, error) {
	duo,err := r.WaitForDuo(ctx,IndexForRoad(types.NextIndexOpt(prev)))
	if err!=nil { return nil,err }
	return &types.ConsensusSpec {
		Epochs: duo,
		CommitQC: prev,
		GenesisFirstBlock: r.genesisFirstBlock,
		GenesisTimestamp: r.genesisTimestamp,
	},nil
}

func (r *Registry) WaitForEpoch(ctx context.Context, i types.EpochIndex) (*types.Epoch, error) {
	for inner,ctrl := range r.state.Lock() {
		for {
			if current, ok := inner[i]; ok {
				return current,nil
			}
			if err:= ctrl.Wait(ctx); err!=nil { return nil,err }
		}
	}
	panic("unreachable")
}


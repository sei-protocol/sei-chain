package epoch

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	epochSnapshotDir    = "epochs"
	epochSnapshotPrefix = "registry"
)

func openEpochSnapshot(
	stateDir utils.Option[string],
) (persist.Persister[*pb.PersistedEpochRegistry], utils.Option[*pb.PersistedEpochRegistry], error) {
	root, ok := stateDir.Get()
	if !ok {
		return persist.NewPersister[*pb.PersistedEpochRegistry](utils.None[string](), epochSnapshotPrefix)
	}
	dir := filepath.Join(root, epochSnapshotDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, utils.None[*pb.PersistedEpochRegistry](), fmt.Errorf("create epoch snapshot dir %s: %w", dir, err)
	}
	return persist.NewPersister[*pb.PersistedEpochRegistry](utils.Some(dir), epochSnapshotPrefix)
}

// checkDerived returns an error if b's committee is not derived from a's.
func checkDerived(a, b *types.Epoch) error {
	committee := b.Committee()
	weights := make(map[types.PublicKey]uint64, committee.Lanes().Len())
	for lane := range committee.Lanes().All() {
		weights[lane.Validator] = committee.Weight(lane.Validator)
	}
	want, err := a.Committee().DeriveNext(weights, b.EpochIndex())
	if err != nil {
		return err
	}
	if !want.Equal(committee) {
		return fmt.Errorf("does not follow epoch %d", a.EpochIndex())
	}
	return nil
}

// persistedRegistry is an immutable, nonempty, contiguous range of live epochs.
// Pending epochs do not count toward the nonempty invariant.
type persistedRegistry struct {
	utils.ReadOnly
	live map[types.EpochIndex]*types.Epoch
	// liveRange is the nonempty retained epoch range [First, Next) in live.
	liveRange types.EpochRange
	pending   utils.Option[*types.Epoch]
}

// newGenesisRegistry constructs the first two live epochs from the genesis committee.
func newGenesisRegistry(committee *types.Committee, firstRoad types.RoadIndex) *persistedRegistry {
	return &persistedRegistry{
		live: map[types.EpochIndex]*types.Epoch{
			0: types.NewEpoch(0, types.RoadRange{First: firstRoad, Next: firstRoad + epochLength}, committee),
			1: types.NewEpoch(1, types.RoadRange{First: firstRoad + epochLength, Next: firstRoad + 2*epochLength}, committee),
		},
		liveRange: types.EpochRange{First: 0, Next: 2},
	}
}

// stageEpoch returns a registry with the committee derived from endEpoch's weights staged as pending.
func (s *persistedRegistry) stageEpoch(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) (*persistedRegistry, error) {
	target := endEpoch + 2
	if target < s.liveRange.First {
		return nil, fmt.Errorf("epoch %d: %w", target, types.ErrPruned)
	}
	ep, registered := s.live[target]
	if !registered && target != s.liveRange.Next {
		return nil, fmt.Errorf("epoch %d cannot be staged, want %d", target, s.liveRange.Next)
	}
	prev, ok := s.live[endEpoch+1]
	if !ok {
		return nil, fmt.Errorf("epoch %d not registered", endEpoch+1)
	}
	committee, err := prev.Committee().DeriveNext(weights, target)
	if err != nil {
		return nil, fmt.Errorf("DeriveNext(%d): %w", target, err)
	}
	if registered {
		if !ep.Committee().Equal(committee) {
			return nil, fmt.Errorf("epoch %d already registered with a different committee", target)
		}
		return s, nil
	}
	if staged, ok := s.pending.Get(); ok {
		if !staged.Committee().Equal(committee) {
			return nil, fmt.Errorf("epoch %d already staged with a different committee", target)
		}
		return s, nil
	}
	first := prev.RoadRange().Next
	pending := types.NewEpoch(target, types.RoadRange{First: first, Next: first + epochLength}, committee)
	return &persistedRegistry{live: s.live, liveRange: s.liveRange, pending: utils.Some(pending)}, nil
}

// activate returns a registry with idx activated and no pending epoch, or the same registry if idx is live.
func (s *persistedRegistry) activate(idx types.EpochIndex) (*persistedRegistry, error) {
	if _, ok := s.live[idx]; ok {
		return s, nil
	}
	if idx < s.liveRange.First {
		return nil, fmt.Errorf("epoch %d: %w", idx, types.ErrPruned)
	}
	pending, ok := s.pending.Get()
	if !ok || pending.EpochIndex() != idx {
		return nil, fmt.Errorf("epoch %d is not staged", idx)
	}
	if idx != s.liveRange.Next {
		return nil, fmt.Errorf("epoch %d cannot be activated, want %d", idx, s.liveRange.Next)
	}
	prev := s.live[idx-1]
	first := prev.RoadRange().Next
	if got, want := pending.RoadRange(), (types.RoadRange{First: first, Next: first + epochLength}); got != want {
		return nil, fmt.Errorf("epoch %d roads %v, want %v", idx, got, want)
	}
	if err := checkDerived(prev, pending); err != nil {
		return nil, fmt.Errorf("epoch %d: %w", idx, err)
	}
	live := maps.Clone(s.live)
	live[idx] = pending
	return &persistedRegistry{
		live:      live,
		liveRange: types.EpochRange{First: s.liveRange.First, Next: idx + 1},
	}, nil
}

// pruneBefore returns a registry retaining epochs from keep and at least the latest live epoch.
func (s *persistedRegistry) pruneBefore(keep types.EpochIndex) *persistedRegistry {
	keep = min(keep, s.liveRange.Next-1)
	if keep <= s.liveRange.First {
		return s
	}
	live := make(map[types.EpochIndex]*types.Epoch, s.liveRange.Next-keep)
	for idx := keep; idx < s.liveRange.Next; idx++ {
		live[idx] = s.live[idx]
	}
	return &persistedRegistry{
		live:      live,
		liveRange: types.EpochRange{First: keep, Next: s.liveRange.Next},
		pending:   s.pending,
	}
}

// persistedRegistryConv converts retained registry state to and from protobuf.
var persistedRegistryConv = protoutils.Conv[*persistedRegistry, *pb.PersistedEpochRegistry]{
	Encode: func(s *persistedRegistry) *pb.PersistedEpochRegistry {
		live := make([]*types.Epoch, 0, len(s.live))
		for idx := s.liveRange.First; idx < s.liveRange.Next; idx++ {
			live = append(live, s.live[idx])
		}
		return &pb.PersistedEpochRegistry{
			LiveV2:    types.EpochConv.EncodeSlice(live),
			PendingV2: types.EpochConv.EncodeOpt(s.pending),
		}
	},
	Decode: func(snapshot *pb.PersistedEpochRegistry) (*persistedRegistry, error) {
		if snapshot == nil {
			return nil, errors.New("missing")
		}
		live, err := types.EpochConv.DecodeSlice(snapshot.LiveV2)
		if err != nil {
			return nil, fmt.Errorf("live: %w", err)
		}
		pending, err := types.EpochConv.DecodeOpt(snapshot.PendingV2)
		if err != nil {
			return nil, fmt.Errorf("pending: %w", err)
		}
		if len(live) == 0 {
			return nil, errors.New("persisted registry has no live epochs")
		}
		for i := 1; i < len(live); i++ {
			prev, epoch := live[i-1], live[i]
			if epoch.EpochIndex() <= prev.EpochIndex() || epoch.EpochIndex() != prev.EpochIndex()+1 {
				return nil, fmt.Errorf("live record %d: epoch %d does not follow %d", i, epoch.EpochIndex(), prev.EpochIndex())
			}
			if prev.RoadRange().Next != epoch.RoadRange().First {
				return nil, fmt.Errorf("live record %d: noncontiguous roads", i)
			}
		}
		if epoch, ok := pending.Get(); ok {
			prev := live[len(live)-1]
			if epoch.EpochIndex() <= prev.EpochIndex() || epoch.EpochIndex() != prev.EpochIndex()+1 {
				return nil, fmt.Errorf("pending epoch %d does not follow %d", epoch.EpochIndex(), prev.EpochIndex())
			}
			if prev.RoadRange().Next != epoch.RoadRange().First {
				return nil, errors.New("pending: noncontiguous roads")
			}
		}
		next := live[len(live)-1].EpochIndex() + 1
		if next == 0 {
			return nil, errors.New("live epoch range overflows")
		}
		epochs := make(map[types.EpochIndex]*types.Epoch, len(live))
		for _, epoch := range live {
			epochs[epoch.EpochIndex()] = epoch
		}
		return &persistedRegistry{
			live:      epochs,
			liveRange: types.EpochRange{First: live[0].EpochIndex(), Next: next},
			pending:   pending,
		}, nil
	},
}

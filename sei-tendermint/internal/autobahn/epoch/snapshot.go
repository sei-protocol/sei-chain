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
type persistedRegistry struct {
	utils.ReadOnly
	live map[types.EpochIndex]*types.Epoch
	// liveRange is the nonempty retained epoch range [First, Next) in live.
	liveRange types.EpochRange
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

// tryPushEpoch returns a registry with the next execution-derived epoch.
func (s *persistedRegistry) tryPushEpoch(n types.RoadIndex, weights map[types.PublicKey]uint64) (*persistedRegistry, error) {
	next := s.liveRange.Next
	last := s.live[next-1]
	if n >= last.RoadRange().First {
		return nil, fmt.Errorf("need weights at %v", last.RoadRange().First-1)
	}
	if n+1 != last.RoadRange().First {
		return s, nil
	}
	committee, err := last.Committee().DeriveNext(weights, next)
	if err != nil {
		return nil, fmt.Errorf("DeriveNext(%d): %w", next, err)
	}
	first := last.RoadRange().Next
	live := maps.Clone(s.live)
	liveRange := s.liveRange
	liveRange.Next = next + 1
	live[next] = types.NewEpoch(next, types.RoadRange{First: first, Next: first + epochLength}, committee)
	return &persistedRegistry{live: live, liveRange: liveRange}, nil
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
			LiveV2: types.EpochConv.EncodeSlice(live),
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
		}, nil
	},
}

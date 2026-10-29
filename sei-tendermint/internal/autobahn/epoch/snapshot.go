package epoch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

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

// persistedRegistry contains a nonempty ordered slice of live epochs and an optional next epoch.
type persistedRegistry struct {
	utils.ReadOnly
	live    []*types.Epoch
	pending utils.Option[*types.Epoch]
}

// newPersistedRegistry constructs a registry with contiguous live and pending epochs.
func newPersistedRegistry(live []*types.Epoch, pending utils.Option[*types.Epoch]) (*persistedRegistry, error) {
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
	return &persistedRegistry{live: slices.Clone(live), pending: pending}, nil
}

// persistedRegistryConv converts retained registry state to and from protobuf.
var persistedRegistryConv = protoutils.Conv[*persistedRegistry, *pb.PersistedEpochRegistry]{
	Encode: func(s *persistedRegistry) *pb.PersistedEpochRegistry {
		return &pb.PersistedEpochRegistry{
			Live:    types.EpochConv.EncodeSlice(s.live),
			Pending: types.EpochConv.EncodeOpt(s.pending),
		}
	},
	Decode: func(snapshot *pb.PersistedEpochRegistry) (*persistedRegistry, error) {
		if snapshot == nil {
			return nil, errors.New("missing")
		}
		live, err := types.EpochConv.DecodeSlice(snapshot.Live)
		if err != nil {
			return nil, fmt.Errorf("live: %w", err)
		}
		pending, err := types.EpochConv.DecodeOpt(snapshot.Pending)
		if err != nil {
			return nil, fmt.Errorf("pending: %w", err)
		}
		return newPersistedRegistry(live, pending)
	},
}

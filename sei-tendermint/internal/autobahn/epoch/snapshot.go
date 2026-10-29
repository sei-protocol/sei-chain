package epoch

import (
	"errors"
	"fmt"
	"maps"
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

// persistedRegistry contains the retained epochs stored on disk.
type persistedRegistry struct {
	live    map[types.EpochIndex]*types.Epoch
	pending utils.Option[*types.Epoch]
}

// persistedRegistryConv converts retained registry state to and from protobuf.
var persistedRegistryConv = protoutils.Conv[persistedRegistry, *pb.PersistedEpochRegistry]{
	Encode: func(s persistedRegistry) *pb.PersistedEpochRegistry {
		out := &pb.PersistedEpochRegistry{
			Live: make([]*pb.Epoch, len(s.live)),
		}
		for i, idx := range slices.Sorted(maps.Keys(s.live)) {
			out.Live[i] = types.EpochConv.Encode(s.live[idx])
		}
		if pending, ok := s.pending.Get(); ok {
			out.Pending = types.EpochConv.Encode(pending)
		}
		return out
	},
	Decode: func(snapshot *pb.PersistedEpochRegistry) (persistedRegistry, error) {
		if snapshot == nil {
			return persistedRegistry{}, errors.New("missing")
		}
		s := persistedRegistry{live: make(map[types.EpochIndex]*types.Epoch, len(snapshot.Live))}
		next := types.EpochIndex(2)
		for i, record := range snapshot.Live {
			epoch, err := types.EpochConv.DecodeReq(record)
			if err != nil {
				return persistedRegistry{}, fmt.Errorf("live record %d: %w", i, err)
			}
			idx := epoch.EpochIndex()
			if i > 0 && idx != next {
				return persistedRegistry{}, fmt.Errorf("live record %d: epoch %d, want %d", i, idx, next)
			}
			if i > 0 && s.live[idx-1].RoadRange().Next != epoch.RoadRange().First {
				return persistedRegistry{}, fmt.Errorf("live record %d: noncontiguous roads", i)
			}
			s.live[idx] = epoch
			next = idx + 1
		}
		if snapshot.Pending != nil {
			epoch, err := types.EpochConv.DecodeReq(snapshot.Pending)
			if err != nil {
				return persistedRegistry{}, fmt.Errorf("pending: %w", err)
			}
			idx := epoch.EpochIndex()
			if idx < 2 || idx != next {
				return persistedRegistry{}, fmt.Errorf("pending epoch %d, want %d", idx, next)
			}
			if prev, ok := s.live[idx-1]; ok && prev.RoadRange().Next != epoch.RoadRange().First {
				return persistedRegistry{}, errors.New("pending: noncontiguous roads")
			}
			s.pending = utils.Some(epoch)
		}
		return s, nil
	},
}

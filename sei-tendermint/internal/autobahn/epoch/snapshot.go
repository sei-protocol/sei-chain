package epoch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
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

func encodeEpochRecord(idx types.EpochIndex, committee *types.Committee) *pb.EpochRecord {
	return &pb.EpochRecord{
		Index:     utils.Alloc(uint64(idx)),
		Committee: types.CommitteeConv.Encode(committee),
	}
}

func decodeEpochRecord(record *pb.EpochRecord) (types.EpochIndex, *types.Committee, error) {
	if record == nil {
		return 0, nil, errors.New("missing")
	}
	if record.Index == nil {
		return 0, nil, errors.New("index: missing")
	}
	idx := types.EpochIndex(*record.Index)
	committee, err := types.CommitteeConv.DecodeReq(record.Committee)
	if err != nil {
		return 0, nil, err
	}
	if err := checkJoinedBy(idx, committee); err != nil {
		return 0, nil, err
	}
	return idx, committee, nil
}

// decodeSnapshotRecord decodes a registry snapshot record. Genesis epochs are
// never persisted, so a record for one is an error.
func decodeSnapshotRecord(record *pb.EpochRecord) (types.EpochIndex, *types.Committee, error) {
	idx, committee, err := decodeEpochRecord(record)
	if err != nil {
		return 0, nil, err
	}
	if idx < 2 {
		return 0, nil, fmt.Errorf("genesis epoch %d", idx)
	}
	return idx, committee, nil
}

// checkJoinedBy returns an error if a member of committee joined after epoch idx.
func checkJoinedBy(idx types.EpochIndex, committee *types.Committee) error {
	for lane := range committee.Lanes().All() {
		if lane.Joined > idx {
			return fmt.Errorf("member joined epoch %d after committee epoch %d", lane.Joined, idx)
		}
	}
	return nil
}

// checkDerivedFromPrev returns an error if committee is not DeriveNext of
// epoch idx-1. A predecessor dropped by PruneBefore cannot be checked and is
// accepted; a predecessor missing for any other reason is an error.
func (s *registryState) checkDerivedFromPrev(idx types.EpochIndex, committee *types.Committee) error {
	prev, ok := s.m[idx-1]
	if !ok {
		if !s.dropped(idx - 1) {
			return fmt.Errorf("missing predecessor epoch %d", idx-1)
		}
		return nil
	}
	weights := make(map[types.PublicKey]uint64, committee.Lanes().Len())
	for lane := range committee.Lanes().All() {
		weights[lane.Validator] = committee.Weight(lane.Validator)
	}
	want, err := prev.Committee().DeriveNext(weights, idx)
	if err != nil {
		return err
	}
	if !want.Equal(committee) {
		return fmt.Errorf("does not follow epoch %d", idx-1)
	}
	return nil
}

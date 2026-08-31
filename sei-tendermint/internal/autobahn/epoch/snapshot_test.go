package epoch

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestRestoreSnapshotValidatesShape(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	c2 := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 2))
	c3 := utils.OrPanic1(c2.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))
	c4 := utils.OrPanic1(c3.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 4))
	restore := func(snapshot *pb.PersistedEpochRegistry) error {
		r := utils.OrPanic1(NewRegistry(genesis, 0, time.Time{}, utils.None[string]()))
		for state := range r.state.Lock() {
			return state.restore(snapshot)
		}
		panic("unreachable")
	}

	require.Error(t, restore(&pb.PersistedEpochRegistry{
		Live: []*pb.EpochRecord{encodeEpochRecord(2, c2), encodeEpochRecord(4, c4)},
	}))
	require.Error(t, restore(&pb.PersistedEpochRegistry{
		Live:    []*pb.EpochRecord{encodeEpochRecord(2, c2)},
		Pending: encodeEpochRecord(4, c4),
	}))
}

func TestRestoreSnapshotRejectsBrokenDerivation(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	other := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1, b: 1}))
	restore := func(snapshot *pb.PersistedEpochRegistry) error {
		r := utils.OrPanic1(NewRegistry(genesis, 0, time.Time{}, utils.None[string]()))
		for state := range r.state.Lock() {
			return state.restore(snapshot)
		}
		panic("unreachable")
	}

	require.Error(t, restore(&pb.PersistedEpochRegistry{
		Live: []*pb.EpochRecord{encodeEpochRecord(2, other)},
	}))
	require.Error(t, restore(&pb.PersistedEpochRegistry{
		Pending: encodeEpochRecord(2, other),
	}))
}

func TestRestoreSnapshotAcceptsDerivedChain(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	c2 := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 2))
	c3 := utils.OrPanic1(c2.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))
	restore := func(snapshot *pb.PersistedEpochRegistry) error {
		r := utils.OrPanic1(NewRegistry(genesis, 0, time.Time{}, utils.None[string]()))
		for state := range r.state.Lock() {
			return state.restore(snapshot)
		}
		panic("unreachable")
	}

	require.NoError(t, restore(&pb.PersistedEpochRegistry{
		Live: []*pb.EpochRecord{encodeEpochRecord(2, c2)},
	}))
	require.NoError(t, restore(&pb.PersistedEpochRegistry{
		Live:    []*pb.EpochRecord{encodeEpochRecord(2, c2)},
		Pending: encodeEpochRecord(3, c3),
	}))
	require.NoError(t, restore(&pb.PersistedEpochRegistry{
		Pending: encodeEpochRecord(2, c2),
	}))
}

func TestRestoreSnapshotAllowsUnchainedFirstLive(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	other := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{b: 1}))
	r := utils.OrPanic1(NewRegistry(genesis, 0, time.Time{}, utils.None[string]()))
	for state := range r.state.Lock() {
		require.NoError(t, state.restore(&pb.PersistedEpochRegistry{
			Live: []*pb.EpochRecord{encodeEpochRecord(3, other)},
		}))
		return
	}
	panic("unreachable")
}

func TestDecodeEpochRecordRejectsFutureJoin(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()
	genesis := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{a: 1}))
	committee := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{a: 1, b: 1}, 3))

	_, _, err := decodeEpochRecord(encodeEpochRecord(2, committee))
	require.Error(t, err)
}

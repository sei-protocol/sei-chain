package epoch

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func makeRegistry(t *testing.T) (*Registry, *types.Committee) {
	t.Helper()
	rng := utils.TestRng()
	committee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		types.GenSecretKey(rng).Public(): 1,
		types.GenSecretKey(rng).Public(): 1,
		types.GenSecretKey(rng).Public(): 1,
	}))
	r := utils.OrPanic1(NewRegistry(committee, 0, time.Time{}))
	return r, committee
}

func TestRegistry_EpochByIndex_UnknownReturnsNotFound(t *testing.T) {
	r, _ := makeRegistry(t)
	if _, ok := r.EpochByIndex(99); ok {
		t.Fatal("EpochByIndex(99) returned ok, want not found")
	}
}

func TestRegistry_EpochByIndex_GenesisFound(t *testing.T) {
	r, _ := makeRegistry(t)
	ep, ok := r.EpochByIndex(0)
	if !ok {
		t.Fatal("EpochByIndex(0) not found")
	}
	if ep.EpochIndex() != 0 {
		t.Fatalf("EpochIndex() = %d, want 0", ep.EpochIndex())
	}
}

func TestRegistry_AddEpoch_ClosesPreviousAndAppends(t *testing.T) {
	rng := utils.TestRng()
	r, _ := makeRegistry(t)
	newCommittee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		types.GenSecretKey(rng).Public(): 1,
	}))
	if err := r.AddEpoch(newCommittee, 10, time.Time{}, 100); err != nil {
		t.Fatalf("AddEpoch: %v", err)
	}
	ep0, ok := r.EpochByIndex(0)
	if !ok {
		t.Fatal("epoch 0 missing after AddEpoch")
	}
	if ep0.Roads().Last != 9 {
		t.Fatalf("epoch 0 roads.Last = %d, want 9", ep0.Roads().Last)
	}
	ep1, ok := r.EpochByIndex(1)
	if !ok {
		t.Fatal("epoch 1 missing after AddEpoch")
	}
	if ep1.Roads().First != 10 {
		t.Fatalf("epoch 1 roads.First = %d, want 10", ep1.Roads().First)
	}
}

func TestRegistry_AddEpoch_RejectsStartBeforeCurrentFirst(t *testing.T) {
	rng := utils.TestRng()
	r, _ := makeRegistry(t)
	newCommittee := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{
		types.GenSecretKey(rng).Public(): 1,
	}))
	if err := r.AddEpoch(newCommittee, 0, time.Time{}, 0); err == nil {
		t.Fatal("AddEpoch with startRoad=0 succeeded, want error")
	}
}

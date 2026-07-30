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

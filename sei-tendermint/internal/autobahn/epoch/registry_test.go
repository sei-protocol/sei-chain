package epoch

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func makeRegistry(t *testing.T) (*Registry, types.SecretKey) {
	t.Helper()
	rng := utils.TestRng()
	key := types.GenSecretKey(rng)
	weights := map[types.PublicKey]uint64{key.Public(): 10}
	committee, err := types.NewCommittee(weights)
	if err != nil {
		t.Fatalf("NewCommittee(): %v", err)
	}
	reg, err := NewRegistry(committee, 0, time.Now())
	if err != nil {
		t.Fatalf("NewRegistry(): %v", err)
	}
	return reg, key
}

func TestRegistry_CommitteeForAlwaysReturnsGenesis(t *testing.T) {
	reg, key := makeRegistry(t)

	for _, r := range []types.RoadIndex{0, 50, 99, 100, 199} {
		c := reg.EpochFor(r).Committee()
		if c == nil {
			t.Fatalf("CommitteeFor(%d) = nil", r)
		}
		if !c.HasReplica(key.Public()) {
			t.Errorf("CommitteeFor(%d): genesis key not in committee", r)
		}
	}
}

func TestNewCommittee_RejectsEmptyWeights(t *testing.T) {
	_, err := types.NewCommittee(map[types.PublicKey]uint64{})
	if err == nil {
		t.Fatal("NewCommittee() succeeded with empty weights, want error")
	}
}

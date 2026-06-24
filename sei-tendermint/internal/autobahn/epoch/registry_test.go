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
	reg, err := NewRegistry(weights, 0, time.Now())
	if err != nil {
		t.Fatalf("NewRegistry(): %v", err)
	}
	return reg, key
}

func TestRegistry_CommitteeForAlwaysReturnsGenesis(t *testing.T) {
	reg, key := makeRegistry(t)

	for _, r := range []types.RoadIndex{0, 50, 99, 100, 199} {
		c := reg.CommitteeFor(r)
		if c == nil {
			t.Fatalf("CommitteeFor(%d) = nil", r)
		}
		if !c.HasReplica(key.Public()) {
			t.Errorf("CommitteeFor(%d): genesis key not in committee", r)
		}
	}
}

func TestNewRegistry_RejectsEmptyWeights(t *testing.T) {
	_, err := NewRegistry(map[types.PublicKey]uint64{}, 0, time.Now())
	if err == nil {
		t.Fatal("NewRegistry() succeeded with empty weights, want error")
	}
}

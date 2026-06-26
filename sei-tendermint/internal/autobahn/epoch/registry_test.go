package epoch

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

func TestNewCommittee_RejectsEmptyWeights(t *testing.T) {
	_, err := types.NewCommittee(map[types.PublicKey]uint64{})
	if err == nil {
		t.Fatal("NewCommittee() succeeded with empty weights, want error")
	}
}

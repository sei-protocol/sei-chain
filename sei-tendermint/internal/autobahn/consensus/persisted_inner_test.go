package consensus

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// genPersistedInner generates a random persistedInner with random optional fields.
func genPersistedInner(rng utils.Rng) *persistedInner {
	p := &persistedInner{}
	if rng.Intn(2) == 1 {
		p.CommitQC = utils.Some(types.GenCommitQC(rng))
	}
	if rng.Intn(2) == 1 {
		p.PrepareQC = utils.Some(types.GenPrepareQC(rng))
	}
	if rng.Intn(2) == 1 {
		p.TimeoutQC = utils.Some(types.GenTimeoutQC(rng))
	}
	if rng.Intn(2) == 1 {
		p.CommitVote = utils.Some(types.GenSigned(rng, types.GenCommitVote(rng)))
	}
	if rng.Intn(2) == 1 {
		p.PrepareVote = utils.Some(types.GenSigned(rng, types.GenPrepareVote(rng)))
	}
	if rng.Intn(2) == 1 {
		p.TimeoutVote = utils.Some(types.GenFullTimeoutVote(rng))
	}
	return p
}

// TestPersistedInnerConv tests the protobuf roundtrip for persistedInner.
func TestPersistedInnerConv(t *testing.T) {
	rng := utils.TestRng()
	// Empty boundary case.
	if err := innerProtoConv.Test(&persistedInner{}); err != nil {
		t.Fatal(err)
	}
	// Random combinations of optional fields.
	for range 10 {
		if err := innerProtoConv.Test(genPersistedInner(rng)); err != nil {
			t.Fatal(err)
		}
	}
}

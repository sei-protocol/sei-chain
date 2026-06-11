package types

import (
	"math"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestNewCommittee_FiltersOutZeroWeightValidators(t *testing.T) {
	rng := utils.TestRng()
	firstBlock := GenGlobalBlockNumber(rng)
	genesisTimestamp := time.Now()
	zeroWeightKey := GenPublicKey(rng)
	nonZeroWeightKey := GenPublicKey(rng)

	committee, err := NewCommittee(map[PublicKey]uint64{
		zeroWeightKey:    0,
		nonZeroWeightKey: 7,
	}, firstBlock, genesisTimestamp)
	if err != nil {
		t.Fatalf("NewCommittee(): %v", err)
	}

	if committee.HasReplica(zeroWeightKey) {
		t.Fatal("HasReplica() = true for zero-weight validator, want false")
	}
	if got := committee.Replicas().Len(); got != 1 {
		t.Fatalf("Replicas().Len() = %v, want 1", got)
	}
	if got := committee.Replicas().At(0); got != nonZeroWeightKey {
		t.Fatalf("Replicas().At(0) = %v, want %v", got, nonZeroWeightKey)
	}
	if got := committee.Weight(nonZeroWeightKey); got != 7 {
		t.Fatalf("Weight() = %v, want 7", got)
	}
}

func TestNewCommittee_RejectsZeroTotalWeight(t *testing.T) {
	rng := utils.TestRng()
	firstBlock := GenGlobalBlockNumber(rng)
	genesisTimestamp := time.Now()

	_, err := NewCommittee(map[PublicKey]uint64{
		GenPublicKey(rng): 0,
		GenPublicKey(rng): 0,
	}, firstBlock, genesisTimestamp)
	if err == nil {
		t.Fatal("NewCommittee() succeeded, want error")
	}
}

func TestNewCommittee_RejectsWeightOverflow(t *testing.T) {
	rng := utils.TestRng()
	firstBlock := GenGlobalBlockNumber(rng)
	genesisTimestamp := time.Now()

	_, err := NewCommittee(map[PublicKey]uint64{
		GenPublicKey(rng): math.MaxUint64,
		GenPublicKey(rng): 1,
	}, firstBlock, genesisTimestamp)
	if err == nil {
		t.Fatal("NewCommittee() succeeded, want error")
	}
}

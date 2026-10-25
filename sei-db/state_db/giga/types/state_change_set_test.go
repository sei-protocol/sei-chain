package types

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func fullChangeSet() *StateChangeSet {
	addr := common.HexToAddress("0x1")
	return &StateChangeSet{
		Balances: []BalanceChange{{Address: addr, Balance: big.NewInt(5), PriorBalance: big.NewInt(3)}},
		Nonces:   []NonceChange{{Address: addr, Nonce: 2, PriorNonce: 1}},
		Code:     []CodeChange{{Address: addr, Code: []byte{0x01}, PriorCode: []byte{0x02}}},
		Storage: []StorageChange{{
			Address:    addr,
			Key:        common.Hash{0x01},
			Value:      common.Hash{0x02},
			PriorValue: common.Hash{0x03},
		}},
	}
}

// A clone must not change when the original is mutated or reset for reuse.
func TestCloneSurvivesTheOriginalBeingReused(t *testing.T) {
	original := fullChangeSet()
	clone := original.Clone()

	original.Balances[0].Balance.SetInt64(999)
	original.Balances[0].PriorBalance.SetInt64(999)
	original.Nonces[0].Nonce = 999
	original.Code[0].Code[0] = 0xff
	original.Code[0].PriorCode[0] = 0xff
	original.Storage[0].Value = common.Hash{0xff}
	original.Reset()

	require.Equal(t, fullChangeSet(), clone)
}

// A clone of no set is no set, and a clone of an empty set is an empty set.
func TestCloneKeepsNilAndEmptyApart(t *testing.T) {
	var nilSet *StateChangeSet
	require.Nil(t, nilSet.Clone())
	clone := (&StateChangeSet{}).Clone()
	require.NotNil(t, clone)
	require.True(t, clone.IsEmpty())
}

func TestCloneTurnsNilBalancesIntoZero(t *testing.T) {
	original := &StateChangeSet{Balances: []BalanceChange{{Address: common.HexToAddress("0x1")}}}
	clone := original.Clone()

	require.Equal(t, new(big.Int), clone.Balances[0].Balance)
	require.Equal(t, new(big.Int), clone.Balances[0].PriorBalance)
}

func TestIsEmptyReportsAnyEntry(t *testing.T) {
	require.True(t, (&StateChangeSet{}).IsEmpty())

	full := fullChangeSet()
	for name, set := range map[string]*StateChangeSet{
		"balances": {Balances: full.Balances},
		"nonces":   {Nonces: full.Nonces},
		"code":     {Code: full.Code},
		"storage":  {Storage: full.Storage},
	} {
		require.False(t, set.IsEmpty(), name)
	}
}

// Reset must zero the entries it drops, so a pooled set does not keep the last block's values alive.
func TestResetEmptiesTheSetAndKeepsCapacity(t *testing.T) {
	set := fullChangeSet()
	balances, code := set.Balances, set.Code

	set.Reset()

	require.True(t, set.IsEmpty())
	require.Equal(t, 1, cap(set.Balances))
	require.Equal(t, 1, cap(set.Nonces))
	require.Equal(t, 1, cap(set.Code))
	require.Equal(t, 1, cap(set.Storage))
	require.Equal(t, BalanceChange{}, balances[0])
	require.Equal(t, CodeChange{}, code[0])
}

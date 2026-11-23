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
		Accounts: []AccountChange{{
			Address: addr,
			Account: AccountState{Balance: big.NewInt(5), Nonce: 2, Code: []byte{0x01}},
			Prior:   AccountState{Balance: big.NewInt(3), Nonce: 1, Code: []byte{0x02}},
		}},
		Storage: []StorageChange{{
			Address:    addr,
			Key:        common.Hash{0x01},
			Value:      common.Hash{0x02},
			PriorValue: common.Hash{0x03},
		}},
	}
}

func TestIsEmptyReportsAnyEntry(t *testing.T) {
	require.True(t, (&StateChangeSet{}).IsEmpty())

	full := fullChangeSet()
	for name, set := range map[string]*StateChangeSet{
		"accounts": {Accounts: full.Accounts},
		"storage":  {Storage: full.Storage},
	} {
		require.False(t, set.IsEmpty(), name)
	}
}

// Reset must zero the entries it drops, so a pooled set does not keep the last block's values alive.
func TestResetEmptiesTheSetAndKeepsCapacity(t *testing.T) {
	set := fullChangeSet()
	accounts := set.Accounts

	set.Reset()

	require.True(t, set.IsEmpty())
	require.Equal(t, 1, cap(set.Accounts))
	require.Equal(t, 1, cap(set.Storage))
	require.Equal(t, AccountChange{}, accounts[0])
}

// Each field is compared on its own, with a nil balance read as zero.
func TestAccountChangeReportsEachChangedField(t *testing.T) {
	unchanged := AccountChange{
		Account: AccountState{Balance: big.NewInt(0), Nonce: 1, Code: []byte{0x01}},
		Prior:   AccountState{Nonce: 1, Code: []byte{0x01}},
	}
	require.False(t, unchanged.BalanceChanged())
	require.False(t, unchanged.NonceChanged())
	require.False(t, unchanged.CodeChanged())

	balance := unchanged
	balance.Account.Balance = big.NewInt(1)
	require.True(t, balance.BalanceChanged())
	require.False(t, balance.NonceChanged())
	require.False(t, balance.CodeChanged())

	nonce := unchanged
	nonce.Account.Nonce = 2
	require.False(t, nonce.BalanceChanged())
	require.True(t, nonce.NonceChanged())
	require.False(t, nonce.CodeChanged())

	code := unchanged
	code.Account.Code = nil
	require.False(t, code.BalanceChanged())
	require.False(t, code.NonceChanged())
	require.True(t, code.CodeChanged())
}

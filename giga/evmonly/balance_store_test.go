package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestPlaceholderBalanceStoreAppliesOwnedBalanceChanges(t *testing.T) {
	address := common.Address{0x11}
	initial := NewMemoryState()
	initial.SetBalance(address, big.NewInt(10))
	store := NewPlaceholderBalanceStore(initial)

	require.Equal(t, big.NewInt(10), store.GetBalance(address))

	updated := big.NewInt(20)
	store.ApplyBalanceChanges([]BalanceChange{{Address: address, Balance: updated}})
	updated.SetInt64(30)
	got := store.GetBalance(address)
	require.Equal(t, big.NewInt(20), got)
	got.SetInt64(40)
	require.Equal(t, big.NewInt(20), store.GetBalance(address))
}

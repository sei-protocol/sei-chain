package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"

	gigabank "github.com/sei-protocol/sei-chain/giga/deps/xbank/keeper"
	cosmosbank "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
)

// TestBankNewAccountInstrumentMirror pins the three mirrored bank_new_account
// instrument declarations to the same meter/name/description/unit.
func TestBankNewAccountInstrumentMirror(t *testing.T) {
	require.Equal(t, BankNewAccountMeter, cosmosbank.BankNewAccountMeter)
	require.Equal(t, BankNewAccountName, cosmosbank.BankNewAccountName)
	require.Equal(t, BankNewAccountDescription, cosmosbank.BankNewAccountDescription)
	require.Equal(t, BankNewAccountUnit, cosmosbank.BankNewAccountUnit)

	require.Equal(t, BankNewAccountMeter, gigabank.BankNewAccountMeter)
	require.Equal(t, BankNewAccountName, gigabank.BankNewAccountName)
	require.Equal(t, BankNewAccountDescription, gigabank.BankNewAccountDescription)
	require.Equal(t, BankNewAccountUnit, gigabank.BankNewAccountUnit)
}

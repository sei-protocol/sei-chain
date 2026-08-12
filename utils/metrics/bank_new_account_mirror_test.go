package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gigabank "github.com/sei-protocol/sei-chain/giga/deps/xbank/keeper"
	cosmosbank "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	util "github.com/sei-protocol/sei-chain/utils/metrics"
)

// TestBankNewAccountInstrumentMirror pins the three mirrored bank_new_account
// instrument declarations to the same meter/name/description/unit.
func TestBankNewAccountInstrumentMirror(t *testing.T) {
	require.Equal(t, util.BankNewAccountMeter, cosmosbank.BankNewAccountMeter)
	require.Equal(t, util.BankNewAccountName, cosmosbank.BankNewAccountName)
	require.Equal(t, util.BankNewAccountDescription, cosmosbank.BankNewAccountDescription)
	require.Equal(t, util.BankNewAccountUnit, cosmosbank.BankNewAccountUnit)

	require.Equal(t, util.BankNewAccountMeter, gigabank.BankNewAccountMeter)
	require.Equal(t, util.BankNewAccountName, gigabank.BankNewAccountName)
	require.Equal(t, util.BankNewAccountDescription, gigabank.BankNewAccountDescription)
	require.Equal(t, util.BankNewAccountUnit, gigabank.BankNewAccountUnit)
}

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConservationAccountsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
  "addresses": ["0x0000000000000000000000000000000000000001"],
  "expected_balances": {
    "0x0000000000000000000000000000000000000001": "42"
  },
  "expected_sum": "42"
}`), 0o600))

	got, err := loadConservationAccountsFile(path)
	require.NoError(t, err)
	require.Equal(t, []string{"0x0000000000000000000000000000000000000001"}, got.Addresses)
	require.Equal(t, map[string]string{
		"0x0000000000000000000000000000000000000001": "42",
	}, got.ExpectedBalances)
	require.Equal(t, "42", got.ExpectedSum)
}

func TestParseWeiDecimal(t *testing.T) {
	got, err := parseWeiDecimal("1606931358149371912483291383930117527567243605732557312714412456632766846976")
	require.NoError(t, err)
	require.Equal(t, evmonlyInitialBalanceWei, got.String())
}

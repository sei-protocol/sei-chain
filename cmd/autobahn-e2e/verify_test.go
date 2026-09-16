package main

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

const (
	firstAddressHex  = "0x0000000000000000000000000000000000000001"
	secondAddressHex = "0x0000000000000000000000000000000000000002"
)

func writeAccountsFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func testConservationOptions(t *testing.T, body string) verifyConservationOptions {
	t.Helper()
	return verifyConservationOptions{
		accountsFile:  writeAccountsFile(t, body),
		initialWei:    evmOnlyInitialBalance().String(),
		maxSampleSize: 64,
	}
}

func TestEVMOnlyInitialBalanceMatchesTheAppBaseBalance(t *testing.T) {
	// evmOnlyBaseBalance in sei-tendermint/internal/evmonlyapp is 1 << 200.
	require.Equal(t,
		"1606938044258990275541962092341162602522202993782792835301376",
		evmOnlyInitialBalance().String())
}

func TestLoadConservationLedgerResolvesExpectedBalancesByAddress(t *testing.T) {
	// The expected_balances key is checksum-cased where the addresses entry is not.
	options := testConservationOptions(t, `{
  "addresses": ["`+firstAddressHex+`"],
  "expected_balances": {"0x0000000000000000000000000000000000000001": "42"},
  "expected_sum": "42"
}`)

	ledger, err := loadConservationLedger(options)
	require.NoError(t, err)
	require.Equal(t, []common.Address{common.HexToAddress(firstAddressHex)}, ledger.addresses)
	require.Equal(t, big.NewInt(42), ledger.expected[common.HexToAddress(firstAddressHex)])
	require.Equal(t, big.NewInt(42), ledger.expectedSum)
}

func TestLoadConservationLedgerRejectsUnlistedExpectedBalance(t *testing.T) {
	options := testConservationOptions(t, `{
  "addresses": ["`+firstAddressHex+`"],
  "expected_balances": {"`+secondAddressHex+`": "42"}
}`)

	_, err := loadConservationLedger(options)
	require.ErrorContains(t, err, "the addresses list does not contain")
}

func TestLoadConservationLedgerRejectsTruncatingASummedFile(t *testing.T) {
	options := testConservationOptions(t, `{
  "addresses": ["`+firstAddressHex+`", "`+secondAddressHex+`"],
  "expected_sum": "84"
}`)
	options.maxSampleSize = 1

	_, err := loadConservationLedger(options)
	require.ErrorContains(t, err, "raise --sample-size or drop expected_sum")
}

func TestLoadConservationLedgerTruncatesAnUnsummedFile(t *testing.T) {
	options := testConservationOptions(t, `{
  "addresses": ["`+firstAddressHex+`", "`+secondAddressHex+`"]
}`)
	options.maxSampleSize = 1

	ledger, err := loadConservationLedger(options)
	require.NoError(t, err)
	require.Equal(t, []common.Address{common.HexToAddress(firstAddressHex)}, ledger.addresses)
}

func TestLoadConservationLedgerRejectsANonAddress(t *testing.T) {
	options := testConservationOptions(t, `{"addresses": ["not-an-address"]}`)

	_, err := loadConservationLedger(options)
	require.ErrorContains(t, err, "is not a hex EVM address")
}

func TestConservationLedgerCheckBalance(t *testing.T) {
	address := common.HexToAddress(firstAddressHex)
	ledger := conservationLedger{
		expected:       map[common.Address]*big.Int{address: big.NewInt(42)},
		initialBalance: big.NewInt(100),
	}

	require.NoError(t, ledger.checkBalance(address, big.NewInt(42)))
	require.ErrorContains(t, ledger.checkBalance(address, big.NewInt(41)), "expected 42")

	unchecked := common.HexToAddress(secondAddressHex)
	require.NoError(t, ledger.checkBalance(unchecked, big.NewInt(99)))
	require.ErrorContains(t, ledger.checkBalance(unchecked, big.NewInt(101)), "exceeds initial funding")
}

func TestConservationLedgerCheckSum(t *testing.T) {
	require.NoError(t, conservationLedger{}.checkSum(big.NewInt(7)))

	ledger := conservationLedger{expectedSum: big.NewInt(84)}
	require.NoError(t, ledger.checkSum(big.NewInt(84)))
	require.ErrorContains(t, ledger.checkSum(big.NewInt(83)), "expected 84")
}

func TestParseWeiDecimal(t *testing.T) {
	got, err := parseWeiDecimal(" 1606938044258990275541962092341162602522202993782792835301376 ")
	require.NoError(t, err)
	require.Equal(t, evmOnlyInitialBalance(), got)

	_, err = parseWeiDecimal("0x2a")
	require.Error(t, err)
}

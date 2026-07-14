//go:build mock_balances

package giga_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/stretchr/testify/require"
)

func TestGigaMockBalancesValidationUsesDBImpl(t *testing.T) {
	blockTime := time.Now()
	accts := utils.NewTestAccounts(3)
	signer := utils.NewSigner()
	recipient := utils.NewSigner()

	gigaCtx := NewGigaTestContext(t, accts, blockTime, 1, ModeGigaSequential)
	gigaCtx.TestApp.GigaEvmKeeper.SetAddressMapping(gigaCtx.Ctx, signer.AccountAddress, signer.EvmAddress)

	to := recipient.EvmAddress
	value := big.NewInt(1_000_000_000_000_000_000)
	fee := big.NewInt(100000000000)
	tx := createCustomEVMTx(t, gigaCtx, signer, &to, value, 21000, fee, fee, 0)

	_, results, err := RunBlock(t, gigaCtx, [][]byte{tx})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, uint32(0), results[0].Code, results[0].Log)
}

package evmrpc

import (
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// Covers the PreExecutionFailure branch of isReceiptUntraceable. Ante-stub /
// shell cases are already exercised by ExcludeTraceFail tests in tx_test.go.
func TestIsReceiptUntraceable_PreExecutionFailure(t *testing.T) {
	floorVmError := "insufficient gas for floor data gas cost: have 27500, want 31000"
	base := types.Receipt{
		Status:            uint32(ethtypes.ReceiptStatusFailed),
		EffectiveGasPrice: 100_000_000_000,
		GasUsed:           27500,
		VmError:           floorVmError,
	}

	withFlag := base
	withFlag.PreExecutionFailure = true
	require.True(t, isReceiptUntraceable(&withFlag))

	withoutFlag := base
	require.False(t, isReceiptUntraceable(&withoutFlag),
		"floor-data VmError alone must not exclude; do not string-match")
}

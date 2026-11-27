package evmonly

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// The fee collector is v2's EVM coinbase.
func TestFeeCollectorAddressIsV2Coinbase(t *testing.T) {
	require.Equal(t, common.HexToAddress("0x27F7B8B8B5A4e71E8E9aA671f4e4031E3773303F"), FeeCollectorAddress)
}

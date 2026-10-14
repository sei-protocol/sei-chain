package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
)

// Every Sei EVM chain config credits the base fee to the coinbase.
func TestEthereumConfigCreditsBaseFeeToCoinbase(t *testing.T) {
	cc := types.DefaultChainConfig()
	require.True(t, cc.EthereumConfig(big.NewInt(1)).SeiCoinbaseReceivesBaseFee)
	sstore := uint64(1)
	require.True(t, cc.EthereumConfigWithSstore(big.NewInt(1), &sstore).SeiCoinbaseReceivesBaseFee)
}

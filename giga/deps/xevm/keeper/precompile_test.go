package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/giga/deps/xevm/keeper"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
)

func toAddr(addr string) *common.Address {
	ca := common.HexToAddress(addr)
	return &ca
}

func TestIsPayablePrecompile(t *testing.T) {
	_, evmAddr := keeper.MockAddressPair()
	require.False(t, evmkeeper.IsPayablePrecompile(&evmAddr))
	require.False(t, evmkeeper.IsPayablePrecompile(nil))

	require.True(t, evmkeeper.IsPayablePrecompile(toAddr(bank.BankAddress)))
	require.True(t, evmkeeper.IsPayablePrecompile(toAddr(staking.StakingAddress)))
	require.True(t, evmkeeper.IsPayablePrecompile(toAddr(gov.GovAddress)))
}

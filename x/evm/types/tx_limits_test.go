package types_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestValidateTxLimits(t *testing.T) {
	to := common.HexToAddress("0x1")
	for _, tc := range []struct {
		name    string
		to      *common.Address
		gas     uint64
		data    int
		wantErr error
	}{
		{name: "gas at EIP-7825 cap", to: &to, gas: params.MaxTxGas},
		{name: "gas over EIP-7825 cap", to: &to, gas: params.MaxTxGas + 1, wantErr: core.ErrGasLimitTooHigh},
		{name: "initcode at limit", gas: 1_000_000, data: params.MaxInitCodeSizeSeiAmsterdam},
		{name: "initcode over limit", gas: 1_000_000, data: params.MaxInitCodeSizeSeiAmsterdam + 1, wantErr: vm.ErrMaxInitCodeSizeExceeded},
		{name: "large calldata to a contract", to: &to, gas: 1_000_000, data: params.MaxInitCodeSizeSeiAmsterdam + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			etx := ethtypes.NewTx(&ethtypes.LegacyTx{To: tc.to, Gas: tc.gas, GasPrice: big.NewInt(1), Data: make([]byte, tc.data)})
			err := types.ValidateTxLimits(etx, 0)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestForkActivation(t *testing.T) {
	require.True(t, types.IsOsaka(0))
	require.True(t, types.IsSeiAmsterdam(0))
	cfg := types.DefaultChainConfig().EthereumConfig(big.NewInt(1))
	require.NoError(t, cfg.CheckConfigForkOrder())
	rules := cfg.Rules(big.NewInt(1), true, 0)
	require.True(t, rules.IsPrague)
	require.True(t, rules.IsOsaka)
	require.True(t, rules.IsSeiAmsterdam)
	require.False(t, rules.IsAmsterdam)
}

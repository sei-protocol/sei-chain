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

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestValidateTxLimits(t *testing.T) {
	to := common.HexToAddress("0x1")
	for _, tc := range []struct {
		name     string
		to       *common.Address
		gas      uint64
		data     int
		wantErr  *sdkerrors.Error
		wantText error
	}{
		{name: "gas at EIP-7825 cap", to: &to, gas: params.MaxTxGas},
		{name: "gas over EIP-7825 cap", to: &to, gas: params.MaxTxGas + 1, wantErr: sdkerrors.ErrOutOfGas, wantText: core.ErrGasLimitTooHigh},
		{name: "initcode at limit", gas: 1_000_000, data: params.MaxInitCodeSizeAmsterdam},
		{name: "initcode over limit", gas: 1_000_000, data: params.MaxInitCodeSizeAmsterdam + 1, wantErr: sdkerrors.ErrTxTooLarge, wantText: vm.ErrMaxInitCodeSizeExceeded},
		{name: "large calldata to a contract", to: &to, gas: 1_000_000, data: params.MaxInitCodeSizeAmsterdam + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			etx := ethtypes.NewTx(&ethtypes.LegacyTx{To: tc.to, Gas: tc.gas, GasPrice: big.NewInt(1), Data: make([]byte, tc.data)})
			rules := types.EthereumRules(0)
			err := types.ValidateTxLimits(etx, &rules)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
			codespace, code, log := sdkerrors.ABCIInfo(err, false)
			require.Equal(t, tc.wantErr.Codespace(), codespace)
			require.Equal(t, tc.wantErr.ABCICode(), code)
			require.Contains(t, log, tc.wantText.Error())
		})
	}
}

func TestForkActivation(t *testing.T) {
	require.NoError(t, types.DefaultChainConfig().EthereumConfig(big.NewInt(1)).CheckConfigForkOrder())
	rules := types.EthereumRules(0)
	require.True(t, rules.IsPrague)
	require.True(t, rules.IsOsaka)
	require.True(t, rules.IsSeiAmsterdam)
	require.False(t, rules.IsAmsterdam)
}

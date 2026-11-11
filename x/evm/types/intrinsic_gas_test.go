package types

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestIntrinsicGas(t *testing.T) {
	to := common.HexToAddress("0x2")
	tests := []struct {
		name    string
		value   *big.Int
		want    uint64
		wantErr bool
	}{
		{name: "plain transfer", value: big.NewInt(1), want: params.TxGas},
		{name: "max uint256 value", value: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)), want: params.TxGas},
		{name: "value over 256 bits", value: new(big.Int).Lsh(big.NewInt(1), 256), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			etx := ethtypes.NewTx(&ethtypes.LegacyTx{GasPrice: big.NewInt(1), Gas: 21000, To: &to, Value: tt.value})
			got, err := IntrinsicGas(etx)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

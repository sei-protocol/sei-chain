package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func TestGetEVMMessageRejectsOversizedFields(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	sender := common.HexToAddress("0x1")
	to := common.HexToAddress("0x2")
	huge := new(big.Int).Lsh(big.NewInt(1), 256)

	tests := []struct {
		name    string
		tx      ethtypes.TxData
		wantErr error
	}{
		{
			name:    "legacy gas price",
			tx:      &ethtypes.LegacyTx{GasPrice: huge, Gas: 21000, To: &to, Value: big.NewInt(0)},
			wantErr: core.ErrFeeCapVeryHigh,
		},
		{
			name:    "dynamic fee tip cap",
			tx:      &ethtypes.DynamicFeeTx{GasFeeCap: big.NewInt(1), GasTipCap: huge, Gas: 21000, To: &to, Value: big.NewInt(0)},
			wantErr: core.ErrTipVeryHigh,
		},
		{
			name: "legacy value",
			tx:   &ethtypes.LegacyTx{GasPrice: big.NewInt(1), Gas: 21000, To: &to, Value: huge},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := k.GetEVMMessage(ctx, ethtypes.NewTx(tt.tx), sender)
			require.Error(t, err)
			require.Nil(t, msg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestGetEVMMessageUsesEffectiveGasPrice(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	to := common.HexToAddress("0x2")
	feeCap := new(big.Int).Lsh(big.NewInt(1), 200)
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{GasFeeCap: feeCap, GasTipCap: big.NewInt(0), Gas: 21000, To: &to, Value: big.NewInt(0)})

	msg, err := k.GetEVMMessage(ctx, tx, common.HexToAddress("0x1"))
	require.NoError(t, err)
	require.Equal(t, k.GetBaseFee(ctx), msg.GasPrice.ToBig())
	require.Equal(t, feeCap, msg.GasFeeCap.ToBig())
}

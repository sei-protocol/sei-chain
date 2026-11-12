package types

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
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
			got, err := IntrinsicGas(etx, EthereumRules(0))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIntrinsicGasToleratesNonPositiveChainID(t *testing.T) {
	to := common.HexToAddress("0x2")
	txs := map[string]ethtypes.TxData{
		"unprotected legacy":     &ethtypes.LegacyTx{GasPrice: big.NewInt(1), Gas: 21000, To: &to, Value: big.NewInt(1), V: big.NewInt(27), R: big.NewInt(1), S: big.NewInt(1)},
		"dynamic fee chain id 0": &ethtypes.DynamicFeeTx{ChainID: big.NewInt(0), GasFeeCap: big.NewInt(1), GasTipCap: big.NewInt(1), Gas: 21000, To: &to, Value: big.NewInt(1)},
	}
	for name, data := range txs {
		t.Run(name, func(t *testing.T) {
			got, err := IntrinsicGas(ethtypes.NewTx(data), EthereumRules(0))
			require.NoError(t, err)
			require.Equal(t, params.TxGas, got)
		})
	}
}

func TestTxSender(t *testing.T) {
	key, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	to := common.HexToAddress("0x2")
	unsigned := &ethtypes.DynamicFeeTx{ChainID: big.NewInt(1329), GasFeeCap: big.NewInt(1), GasTipCap: big.NewInt(1), Gas: 21000, To: &to, Value: big.NewInt(1)}
	signed, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(big.NewInt(1329)), unsigned)
	require.NoError(t, err)
	badSig := *unsigned
	badSig.R, badSig.S, badSig.V = big.NewInt(1), big.NewInt(0), big.NewInt(0)

	tests := []struct {
		name    string
		tx      *ethtypes.Transaction
		want    common.Address
		wantErr bool
	}{
		{name: "unsigned", tx: ethtypes.NewTx(unsigned), want: common.Address{}},
		{name: "signed", tx: signed, want: crypto.PubkeyToAddress(key.PublicKey)},
		{name: "invalid signature", tx: ethtypes.NewTx(&badSig), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := txSender(tt.tx)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEthereumRulesAtGenesis(t *testing.T) {
	rules := EthereumRules(0)
	require.True(t, rules.IsHomestead)
	require.True(t, rules.IsIstanbul)
	require.True(t, rules.IsShanghai)
	require.True(t, rules.IsPrague)
	require.False(t, rules.IsAmsterdam)
}

package evm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestSignerFeeWidths(t *testing.T) {
	chainID := big.NewInt(testChainID)
	maxFee := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	cases := []struct {
		name string
		tx   func(*big.Int) ethtypes.TxData
		err  error
	}{
		{
			name: "legacy gas price",
			tx: func(fee *big.Int) ethtypes.TxData {
				return &ethtypes.LegacyTx{Gas: 21_000, GasPrice: fee}
			},
			err: core.ErrFeeCapVeryHigh,
		},
		{
			name: "access list gas price",
			tx: func(fee *big.Int) ethtypes.TxData {
				return &ethtypes.AccessListTx{ChainID: chainID, Gas: 21_000, GasPrice: fee}
			},
			err: core.ErrFeeCapVeryHigh,
		},
		{
			name: "dynamic fee cap",
			tx: func(fee *big.Int) ethtypes.TxData {
				return &ethtypes.DynamicFeeTx{ChainID: chainID, Gas: 21_000, GasFeeCap: fee, GasTipCap: new(big.Int)}
			},
			err: core.ErrFeeCapVeryHigh,
		},
		{
			name: "dynamic tip cap",
			tx: func(fee *big.Int) ethtypes.TxData {
				return &ethtypes.DynamicFeeTx{ChainID: chainID, Gas: 21_000, GasFeeCap: maxFee, GasTipCap: fee}
			},
			err: core.ErrTipVeryHigh,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, boundary := range []struct {
				name     string
				fee      *big.Int
				overflow bool
			}{
				{name: "zero", fee: new(big.Int)},
				{name: "max uint256", fee: maxFee},
				{name: "overflow", fee: new(big.Int).Add(maxFee, big.NewInt(1)), overflow: true},
			} {
				t.Run(boundary.name, func(t *testing.T) {
					key, err := crypto.GenerateKey()
					require.NoError(t, err)
					sender := crypto.PubkeyToAddress(key.PublicKey)
					raw := signParserTestTx(t, key, chainID, tc.tx(boundary.fee))
					tx := decodeTx(t, raw)
					signer := ethtypes.LatestSignerForChainID(chainID)
					err = NewSigner(signer, 0, 1).Admits(tx)
					if boundary.overflow {
						require.ErrorIs(t, err, tc.err)
					} else {
						require.NoError(t, err)
					}
					for _, cached := range []bool{false, true} {
						parser := NewSigner(signer, 1, 2)
						if cached {
							parser.Remember(TestEvmTx(tx, sender))
						}
						parsed, singleErr := parser.ParseTx(raw)
						batch, batchErr := parser.ParseTxs([]RawTx{raw, raw})
						if boundary.overflow {
							require.ErrorIs(t, singleErr, tc.err)
							require.ErrorIs(t, batchErr, tc.err)
							require.Nil(t, batch)
						} else {
							require.NoError(t, singleErr)
							require.NoError(t, batchErr)
							require.Equal(t, sender, parsed.Sender())
							require.Len(t, batch, 2)
							for _, decoded := range batch {
								require.Equal(t, sender, decoded.Sender())
							}
						}
					}
				})
			}
		})
	}
}

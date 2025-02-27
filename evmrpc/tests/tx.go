package tests

import (
	"math/big"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func send(nonce uint64) ethtypes.TxData {
	_, recipient := testkeeper.MockAddressPair()
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       21000,
		To:        &recipient,
		Value:     big.NewInt(2000),
		Data:      []byte{},
		ChainID:   chainId,
	}
}

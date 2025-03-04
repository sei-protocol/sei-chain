package tests

import (
	"fmt"
	"math/big"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/precompiles"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
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

func registerCW20Pointer(nonce uint64, cw20Addr string) ethtypes.TxData {
	pInfo := precompiles.GetPrecompileInfo(pointer.PrecompileName)
	input, _ := pInfo.ABI.Pack("addCW20Pointer", cw20Addr)
	pointer := common.HexToAddress(pointer.PointerAddress)
	return &ethtypes.DynamicFeeTx{
		Nonce:     0,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       4000000,
		To:        &pointer,
		Value:     big.NewInt(0),
		Data:      input,
		ChainID:   chainId,
	}
}

func transferCW20Msg(mnemonic string, cw20Addr string) sdk.Msg {
	recipient, _ := testkeeper.MockAddressPair()
	return &wasmtypes.MsgExecuteContract{
		Sender:   getSeiAddrWithMnemonic(mnemonic).String(),
		Contract: cw20Addr,
		Msg:      []byte(fmt.Sprintf("{\"transfer\":{\"recipient\":\"%s\",\"amount\":\"100\"}}", recipient.String())),
	}
}

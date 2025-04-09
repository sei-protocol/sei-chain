package tests

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"strings"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/precompiles"
	"github.com/sei-protocol/sei-chain/precompiles/json"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/wsei"
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

func sendErc20(nonce uint64) ethtypes.TxData {
	_, recipient := testkeeper.MockAddressPair()
	parsedABI, _ := abi.JSON(strings.NewReader(string(wsei.GetABI())))
	bz, _ := parsedABI.Methods["transfer"].Inputs.Pack(recipient, 1)
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       100000,
		To:        &erc20Addr,
		Data:      bz,
		ChainID:   chainId,
	}
}

func depositErc20(nonce uint64) ethtypes.TxData {
	parsedABI, _ := abi.JSON(strings.NewReader(string(wsei.GetABI())))
	bz, _ := parsedABI.Methods["deposit"].Inputs.Pack()
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       100000,
		To:        &erc20Addr,
		Value:     big.NewInt(1),
		Data:      bz,
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

func jsonExtractAsBytesFromArray(nonce uint64) ethtypes.TxData {
	abiBz, err := os.ReadFile("../../precompiles/json/abi.json")
	if err != nil {
		panic(err)
	}
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		panic(err)
	}
	input, err := newAbi.Pack("extractAsBytesFromArray", []byte("[\"1\"]"), uint16(0))
	if err != nil {
		panic(err)
	}
	to := common.HexToAddress(json.JSONAddress)
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       50000,
		To:        &to,
		Data:      input,
		ChainID:   chainId,
	}
}

func bankSendMsg(mnemonic string) sdk.Msg {
	recipient, _ := testkeeper.MockAddressPair()
	return &banktypes.MsgSend{
		FromAddress: getSeiAddrWithMnemonic(mnemonic).String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt())),
	}
}

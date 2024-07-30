package evmrpc_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/example/contracts/simplestorage"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestEstimateGas(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	// transfer
	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      to.Hex(),
		"value":   "0x10",
		"nonce":   "0x1",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(20)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(from[:]), amts)
	resObj := sendRequestGood(t, "estimateGas", txArgs, nil, map[string]interface{}{})
	result := resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000
	resObj = sendRequestGood(t, "estimateGas", txArgs, "latest", map[string]interface{}{})
	result = resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000
	resObj = sendRequestGood(t, "estimateGas", txArgs, "0x123456", map[string]interface{}{})
	result = resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000

	// contract call
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs = map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	resObj = sendRequestGood(t, "estimateGas", txArgs, nil, map[string]interface{}{})
	result = resObj["result"].(string)
	require.Equal(t, "0x53f9", result) // 21497

	Ctx = Ctx.WithBlockHeight(8)
}

func TestCreateAccessList(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)

	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x1",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(20)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(from[:]), amts)
	resObj := sendRequestGood(t, "createAccessList", txArgs, "latest")
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, []interface{}{}, result["accessList"]) // the code uses MSTORE which does not trace access list

	resObj = sendRequestBad(t, "createAccessList", txArgs, "latest")
	result = resObj["error"].(map[string]interface{})
	require.Equal(t, "error block", result["message"])

	Ctx = Ctx.WithBlockHeight(8)
}

func TestCall(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)

	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	resObj := sendRequestGood(t, "call", txArgs, nil, map[string]interface{}{}, map[string]interface{}{})
	result := resObj["result"].(string)
	require.Equal(t, "0x608060405234801561000f575f80fd5b506004361061003f575f3560e01c806360fe47b1146100435780636d4ce63c1461005f5780639c3674fc1461007d575b5f80fd5b61005d6004803603810190610058919061010a565b610087565b005b6100676100c7565b6040516100749190610144565b60405180910390f35b6100856100cf565b005b805f819055507f0de2d86113046b9e8bb6b785e96a6228f6803952bf53a40b68a36dce316218c1816040516100bc9190610144565b60405180910390a150565b5f8054905090565b5f80fd5b5f80fd5b5f819050919050565b6100e9816100d7565b81146100f3575f80fd5b50565b5f81359050610104816100e0565b92915050565b5f6020828403121561011f5761011e6100d3565b5b5f61012c848285016100f6565b91505092915050565b61013e816100d7565b82525050565b5f6020820190506101575f830184610135565b9291505056fea2646970667358221220bb55137839ea2afda11ab2d30ad07fee30bb9438caaa46e30ccd1053ed72439064736f6c63430008150033", result)

	Ctx = Ctx.WithBlockHeight(8)
}

func TestEthCallHighAmount(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      to.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}

	overrides := map[string]map[string]interface{}{
		from.Hex(): {"balance": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
	}
	resObj := sendRequestGood(t, "call", txArgs, "latest", overrides)
	fmt.Println("resObj = ", resObj)
	errMap := resObj["error"].(map[string]interface{})
	result := errMap["message"]
	fmt.Println("res = ", result)
	require.Equal(t, result, "error: balance override overflow")

	Ctx = Ctx.WithBlockHeight(8)
}

func TestNewRevertError(t *testing.T) {
	err := evmrpc.NewRevertError(&core.ExecutionResult{})
	require.NotNil(t, err)
	require.Equal(t, 3, err.ErrorCode())
	require.Equal(t, "0x", err.ErrorData())
}

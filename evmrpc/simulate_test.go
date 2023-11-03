package evmrpc_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/keeper/testdata"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestEstimateGas(t *testing.T) {
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
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(20))))
	EVMKeeper.SetOrDeleteBalance(Ctx, from, 20)
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
	code, err := os.ReadFile("../x/evm/keeper/testdata/SimpleStorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := testdata.TestdataMetaData.GetAbi()
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
	require.Equal(t, "0x534d", result) // 21325
}

func TestCreateAccessList(t *testing.T) {
	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../x/evm/keeper/testdata/SimpleStorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := testdata.TestdataMetaData.GetAbi()
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
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(20))))
	EVMKeeper.SetOrDeleteBalance(Ctx, from, 20)
	resObj := sendRequestGood(t, "createAccessList", txArgs, "latest")
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, []interface{}{}, result["accessList"]) // the code uses MSTORE which does not trace access list

	resObj = sendRequestBad(t, "createAccessList", txArgs, "latest")
	result = resObj["error"].(map[string]interface{})
	require.Equal(t, "error block", result["message"])
}

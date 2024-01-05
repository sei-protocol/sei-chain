package keeper_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestInternalCallCreateContract(t *testing.T) {
	bytecode := artifacts.GetNativeSeiTokensERC20Bin()
	abi, err := artifacts.ArtifactsMetaData.GetAbi()
	require.Nil(t, err)
	args, err := abi.Pack("", "test")
	require.Nil(t, err)
	contractData := append(bytecode, args...)

	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	testAddr, _ := testkeeper.MockAddressPair()
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, testAddr, amt))
	req := &types.MsgInternalEVMCall{
		Sender: testAddr.String(),
		Data:   contractData,
	}
	_, err = k.HandleInternalEVMCall(ctx, req)
	require.Nil(t, err)
}

func TestInternalCall(t *testing.T) {
	bytecode := artifacts.GetNativeSeiTokensERC20Bin()
	abi, err := artifacts.ArtifactsMetaData.GetAbi()
	require.Nil(t, err)
	args, err := abi.Pack("", "test")
	require.Nil(t, err)
	contractData := append(bytecode, args...)

	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	testAddr, senderEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, testAddr, senderEvmAddr)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, testAddr, amt))
	req := &types.MsgInternalEVMCall{
		Sender: testAddr.String(),
		Data:   contractData,
	}
	ret, err := k.HandleInternalEVMCall(ctx, req)
	require.Nil(t, err)
	contractAddr := crypto.CreateAddress(senderEvmAddr, 0)
	require.NotEmpty(t, k.GetCode(ctx, contractAddr))
	require.Equal(t, ret.Data, k.GetCode(ctx, contractAddr))

	receiverAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, receiverAddr, evmAddr)
	args, err = abi.Pack("transfer", evmAddr, big.NewInt(1000))
	require.Nil(t, err)
	require.Nil(t, k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("test", sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, testAddr, sdk.NewCoins(sdk.NewCoin("test", sdk.NewInt(200000000)))))
	val := sdk.NewInt(0)
	req = &types.MsgInternalEVMCall{
		Sender: testAddr.String(),
		To:     sdk.AccAddress(contractAddr[:]).String(),
		Data:   args,
		Value:  &val,
	}
	_, err = k.HandleInternalEVMCall(ctx, req)
	require.Nil(t, err)
	require.Equal(t, int64(1000), testkeeper.EVMTestApp.BankKeeper.GetBalance(ctx, receiverAddr, "test").Amount.Int64())
}

func TestStaticCall(t *testing.T) {
	bytecode := artifacts.GetNativeSeiTokensERC20Bin()
	abi, err := artifacts.ArtifactsMetaData.GetAbi()
	require.Nil(t, err)
	args, err := abi.Pack("", "test")
	require.Nil(t, err)
	contractData := append(bytecode, args...)

	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	testAddr, senderEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, testAddr, senderEvmAddr)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, testAddr, amt))
	req := &types.MsgInternalEVMCall{
		Sender: testAddr.String(),
		Data:   contractData,
	}
	ret, err := k.HandleInternalEVMCall(ctx, req)
	require.Nil(t, err)
	contractAddr := crypto.CreateAddress(senderEvmAddr, 0)
	require.NotEmpty(t, k.GetCode(ctx, contractAddr))
	require.Equal(t, ret.Data, k.GetCode(ctx, contractAddr))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("test", sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, testAddr, sdk.NewCoins(sdk.NewCoin("test", sdk.NewInt(200000000)))))

	args, err = abi.Pack("balanceOf", senderEvmAddr)
	require.Nil(t, err)
	res, err := k.StaticCallEVM(ctx, testAddr, &contractAddr, args)
	require.Nil(t, err)
	decoded, err := abi.Unpack("balanceOf", res)
	require.Nil(t, err)
	require.Equal(t, 1, len(decoded))
	require.Equal(t, big.NewInt(200000000), decoded[0].(*big.Int))
}

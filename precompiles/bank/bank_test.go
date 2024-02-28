package bank_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestRun(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)

	// Setup receiving addresses
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	p, err := bank.NewPrecompile(k.BankKeeper(), k)
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	// Precompile send test
	send, err := p.ABI.MethodById(p.SendID)
	require.Nil(t, err)
	args, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "usei", big.NewInt(25))
	require.Nil(t, err)
	_, err = p.Run(&evm, senderEVMAddr, append(p.SendID, args...), nil) // should error because address is not whitelisted
	require.NotNil(t, err)

	// Precompile sendNative test error
	sendNative, err := p.ABI.MethodById(p.SendNativeID)
	require.Nil(t, err)
	seiAddrString := seiAddr.String()
	argsNativeError, err := sendNative.Inputs.Pack(seiAddrString)
	require.Nil(t, err)
	// 0 amount disallowed
	_, err = p.Run(&evm, senderEVMAddr, append(p.SendNativeID, argsNativeError...), big.NewInt(0))
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack("")
	require.Nil(t, err)
	_, err = p.Run(&evm, senderEVMAddr, append(p.SendNativeID, argsNativeError...), big.NewInt(100))
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack("invalidaddr")
	require.Nil(t, err)
	_, err = p.Run(&evm, senderEVMAddr, append(p.SendNativeID, argsNativeError...), big.NewInt(100))
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack(senderAddr.String())
	require.Nil(t, err)
	_, err = p.Run(&evm, evmAddr, append(p.SendNativeID, argsNativeError...), big.NewInt(100))
	require.NotNil(t, err)

	// Send native 10_000_000_000_100, split into 10 usei 100wei
	// Test payable with eth LegacyTx
	abi := bank.GetABI()
	argsNative, err := abi.Pack(bank.SendNativeMethod, seiAddr.String())
	require.Nil(t, err)
	require.Nil(t, err)
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(bank.BankAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(100000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(10_000_000_000_100),
		Data:     argsNative,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	msgServer := keeper.NewMsgServerImpl(k)
	ante.Preprocess(ctx, req)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	// Use precompile balance to verify sendNative usei amount succeeded
	balance, err := p.ABI.MethodById(p.BalanceID)
	require.Nil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "usei")
	require.Nil(t, err)
	precompileRes, err := p.Run(&evm, common.Address{}, append(p.BalanceID, args...), nil)
	require.Nil(t, err)
	is, err := balance.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(is))
	require.Equal(t, big.NewInt(10), is[0].(*big.Int))
	weiBalance := k.BankKeeper().GetWeiBalance(ctx, seiAddr)
	require.Equal(t, big.NewInt(100), weiBalance.BigInt())

	// Verify errors properly raised on bank balance calls with incorrect inputs
	_, err = p.Run(&evm, common.Address{}, append(p.BalanceID, args[:1]...), nil)
	require.NotNil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "")
	require.Nil(t, err)
	_, err = p.Run(&evm, common.Address{}, append(p.BalanceID, args...), nil)
	require.NotNil(t, err)

	// invalid input
	_, err = p.Run(&evm, common.Address{}, []byte{1, 2, 3, 4}, nil)
	require.NotNil(t, err)
}

func TestMetadata(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	k.BankKeeper().SetDenomMetaData(ctx, banktypes.Metadata{Name: "SEI", Symbol: "usei", Base: "usei"})
	p, err := bank.NewPrecompile(k.BankKeeper(), k)
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	name, err := p.ABI.MethodById(p.NameID)
	require.Nil(t, err)
	args, err := name.Inputs.Pack("usei")
	require.Nil(t, err)
	res, err := p.Run(&evm, common.Address{}, append(p.NameID, args...), nil)
	require.Nil(t, err)
	outputs, err := name.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, "SEI", outputs[0])

	symbol, err := p.ABI.MethodById(p.SymbolID)
	require.Nil(t, err)
	args, err = symbol.Inputs.Pack("usei")
	require.Nil(t, err)
	res, err = p.Run(&evm, common.Address{}, append(p.SymbolID, args...), nil)
	require.Nil(t, err)
	outputs, err = symbol.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, "usei", outputs[0])

	decimal, err := p.ABI.MethodById(p.DecimalsID)
	require.Nil(t, err)
	args, err = decimal.Inputs.Pack("usei")
	require.Nil(t, err)
	res, err = p.Run(&evm, common.Address{}, append(p.DecimalsID, args...), nil)
	require.Nil(t, err)
	outputs, err = decimal.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, uint8(0), outputs[0])

	supply, err := p.ABI.MethodById(p.SupplyID)
	require.Nil(t, err)
	args, err = supply.Inputs.Pack("usei")
	require.Nil(t, err)
	res, err = p.Run(&evm, common.Address{}, append(p.SupplyID, args...), nil)
	require.Nil(t, err)
	outputs, err = supply.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, big.NewInt(10), outputs[0])
}

func TestRequiredGas(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	p, err := bank.NewPrecompile(k.BankKeeper(), k)
	require.Nil(t, err)
	balanceRequiredGas := p.RequiredGas(p.BalanceID)
	require.Equal(t, uint64(1000), balanceRequiredGas)
	// invalid method
	require.Equal(t, uint64(0), p.RequiredGas([]byte{1, 1, 1, 1}))
}

func TestAddress(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	p, err := bank.NewPrecompile(k.BankKeeper(), k)
	require.Nil(t, err)
	require.Equal(t, common.HexToAddress(bank.BankAddress), p.Address())
}

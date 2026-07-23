package bank_test

import (
	"embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

//go:embed abi.json
var f embed.FS

type mockTx struct {
	msgs    []sdk.Msg
	signers []sdk.AccAddress
}

func (tx mockTx) GetMsgs() []sdk.Msg                              { return tx.msgs }
func (tx mockTx) ValidateBasic() error                            { return nil }
func (tx mockTx) GetSigners() []sdk.AccAddress                    { return tx.signers }
func (tx mockTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (tx mockTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }
func (tx mockTx) GetGasEstimate() uint64                          { return 0 }

func TestRun(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	upgradeKeeper := &testApp.UpgradeKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("ufoo", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("ufoo", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)

	// Setup receiving addresses
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	// Precompile send test
	send, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SendID)
	require.Nil(t, err)
	args, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "usei", big.NewInt(25))
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, nil, nil, true, false) // should error because of read only call
	require.NotNil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, big.NewInt(1), nil, false, false) // should error because it's not payable
	require.NotNil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, nil, nil, false, false) // should error because address is not whitelisted
	require.NotNil(t, err)
	invalidDenomArgs, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "", big.NewInt(25))
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, invalidDenomArgs...), 100000, nil, nil, false, false) // should error because denom is empty
	require.NotNil(t, err)

	// Precompile sendNative test error
	sendNative, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID)
	require.Nil(t, err)
	seiAddrString := seiAddr.String()
	argsNativeError, err := sendNative.Inputs.Pack(seiAddrString)
	require.Nil(t, err)
	// 0 amount disallowed
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(0), nil, false, false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack("")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, false, false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack("invalidaddr")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, false, false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack(senderAddr.String())
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, false, false)
	require.NotNil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, true, false)
	require.NotNil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), 100000, big.NewInt(100), nil, false, true)
	require.NotNil(t, err)

	// Send native 10_000_000_000_100, split into 10 usei 100wei
	// Test payable with eth LegacyTx
	abi := pcommon.MustGetABI(f, "abi.json")
	argsNative, err := abi.Pack(bank.SendNativeMethod, seiAddr.String())
	require.Nil(t, err)
	require.Nil(t, err)
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(bank.BankAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
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

	// send the transaction
	msgServer := keeper.NewMsgServerImpl(k)
	ante.Preprocess(ctx, req, k.ChainID(ctx), false)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	ctx, err = ante.NewEVMFeeCheckDecorator(k, upgradeKeeper).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	evts := ctx.EventManager().ABCIEvents()

	for _, evt := range evts {
		var lines []string
		for _, attr := range evt.Attributes {
			lines = append(lines, fmt.Sprintf("%s=%s", string(attr.Key), string(attr.Value)))
		}
		fmt.Printf("type=%s\t%s\n", evt.Type, strings.Join(lines, "\t"))
	}

	var expectedEvts sdk.Events = []sdk.Event{
		// gas is sent from sender
		banktypes.NewCoinSpentEvent(senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(200000)))),
		// wei events
		banktypes.NewWeiSpentEvent(senderAddr, sdk.NewInt(100)),
		banktypes.NewWeiReceivedEvent(seiAddr, sdk.NewInt(100)),
		sdk.NewEvent(
			banktypes.EventTypeWeiTransfer,
			sdk.NewAttribute(banktypes.AttributeKeyRecipient, seiAddr.String()),
			sdk.NewAttribute(banktypes.AttributeKeySender, senderAddr.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, sdk.NewInt(100).String()),
		),
		// sender sends coin to the receiver
		banktypes.NewCoinSpentEvent(senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))),
		banktypes.NewCoinReceivedEvent(seiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))),
		sdk.NewEvent(
			banktypes.EventTypeTransfer,
			sdk.NewAttribute(banktypes.AttributeKeyRecipient, seiAddr.String()),
			sdk.NewAttribute(banktypes.AttributeKeySender, senderAddr.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, sdk.NewCoin("usei", sdk.NewInt(10)).String()),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(banktypes.AttributeKeySender, senderAddr.String()),
		),
		// gas refund to the sender
		banktypes.NewCoinReceivedEvent(senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(132401)))),
		// tip is paid to the validator
		banktypes.NewCoinReceivedEvent(sdk.MustAccAddressFromBech32("sei1v4mx6hmrda5kucnpwdjsqqqqqqqqqqqqlve8dv"), sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(67599)))),
	}
	require.EqualValues(t, expectedEvts.ToABCIEvents(), evts)

	// Use precompile balance to verify sendNative usei amount succeeded
	balance, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID)
	require.Nil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "usei")
	require.Nil(t, err)
	precompileRes, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	is, err := balance.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(is))
	require.Equal(t, big.NewInt(10), is[0].(*big.Int))
	weiBalance := k.BankKeeper().GetWeiBalance(ctx, seiAddr)
	require.Equal(t, big.NewInt(100), weiBalance.BigInt())

	newAddr, _ := testkeeper.MockAddressPair()
	require.Nil(t, k.AccountKeeper().GetAccount(ctx, newAddr))
	argsNewAccount, err := sendNative.Inputs.Pack(newAddr.String())
	require.Nil(t, err)
	require.Nil(t, k.BankKeeper().SendCoins(ctx, seiAddr, k.GetSeiAddressOrDefault(ctx, p.Address()), sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt()))))
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNewAccount...), 100000, big.NewInt(1), nil, false, false)
	require.Nil(t, err)
	// should create account if not exists
	require.NotNil(t, k.AccountKeeper().GetAccount(statedb.Ctx(), newAddr))

	// test get all balances
	allBalances, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID)
	require.Nil(t, err)
	args, err = allBalances.Inputs.Pack(senderEVMAddr)
	require.Nil(t, err)
	precompileRes, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	balances, err := allBalances.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(balances))
	parsedBalances := balances[0].([]struct {
		Amount *big.Int `json:"amount"`
		Denom  string   `json:"denom"`
	})

	require.Equal(t, 2, len(parsedBalances))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(10000000),
		Denom:  "ufoo",
	}, bank.CoinBalance(parsedBalances[0]))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(9932390),
		Denom:  "usei",
	}, bank.CoinBalance(parsedBalances[1]))

	// Verify errors properly raised on bank balance calls with incorrect inputs
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args[:1]...), 100000, nil, nil, false, false)
	require.NotNil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), 100000, nil, nil, false, false)
	require.NotNil(t, err)

	// invalid input
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, []byte{1, 2, 3, 4}, 100000, nil, nil, false, false)
	require.NotNil(t, err)
}

func TestSendForUnlinkedReceiver(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	// testPrivHex := hex.EncodeToString(privKey.Bytes())
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("ufoo", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("ufoo", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)

	_, pointerAddr := testkeeper.MockAddressPair()
	k.SetERC20NativePointer(ctx, "ufoo", pointerAddr)

	// Setup receiving addresses - NOT linked
	_, evmAddr := testkeeper.MockAddressPair()
	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	// Precompile send test
	send, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SendID)
	require.Nil(t, err)
	args, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "ufoo", big.NewInt(100))
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, pointerAddr, pointerAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), 100000, nil, nil, false, false) // should not error
	require.Nil(t, err)

	// Use precompile balance to verify sendNative usei amount succeeded
	balance, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID)
	require.Nil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "ufoo")
	require.Nil(t, err)
	precompileRes, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	is, err := balance.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(is))
	require.Equal(t, big.NewInt(100), is[0].(*big.Int))

	// test get all balances
	allBalances, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID)
	require.Nil(t, err)
	args, err = allBalances.Inputs.Pack(senderEVMAddr)
	require.Nil(t, err)
	precompileRes, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	balances, err := allBalances.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(balances))
	parsedBalances := balances[0].([]struct {
		Amount *big.Int `json:"amount"`
		Denom  string   `json:"denom"`
	})

	require.Equal(t, 2, len(parsedBalances))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(9999900),
		Denom:  "ufoo",
	}, bank.CoinBalance(parsedBalances[0]))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(10000000),
		Denom:  "usei",
	}, bank.CoinBalance(parsedBalances[1]))

	// Verify errors properly raised on bank balance calls with incorrect inputs
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args[:1]...), 100000, nil, nil, false, false)
	require.NotNil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), 100000, nil, nil, false, false)
	require.NotNil(t, err)

	// invalid input
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, []byte{1, 2, 3, 4}, 100000, nil, nil, false, false)
	require.NotNil(t, err)
}

func TestMetadata(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	k.BankKeeper().SetDenomMetaData(ctx, banktypes.Metadata{Name: "SEI", Symbol: "usei", Base: "usei"})
	p, err := bank.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	name, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).NameID)
	require.Nil(t, err)
	args, err := name.Inputs.Pack("usei")
	require.Nil(t, err)
	res, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).NameID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := name.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, "SEI", outputs[0])

	symbol, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SymbolID)
	require.Nil(t, err)
	args, err = symbol.Inputs.Pack("usei")
	require.Nil(t, err)
	res, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).SymbolID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err = symbol.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, "usei", outputs[0])

	decimal, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).DecimalsID)
	require.Nil(t, err)
	args, err = decimal.Inputs.Pack("usei")
	require.Nil(t, err)
	res, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).DecimalsID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err = decimal.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, uint8(0), outputs[0])
}

func TestAddress(t *testing.T) {
	p, err := bank.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.Nil(t, err)
	require.Equal(t, common.HexToAddress(bank.BankAddress), p.Address())
}

func TestSpendableBalancesQuery(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("uspendablequery", sdk.NewInt(5000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, sdk.NewCoins(sdk.NewCoin("uspendablequery", sdk.NewInt(5000))))
	require.Nil(t, err)

	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	spendableBalances, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SpendableBalancesID)
	require.Nil(t, err)
	args, err := spendableBalances.Inputs.Pack(evmAddr, []byte{})
	require.Nil(t, err)
	res, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).SpendableBalancesID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := spendableBalances.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 2, len(outputs))
	balances := outputs[0].([]struct {
		Amount *big.Int `json:"amount"`
		Denom  string   `json:"denom"`
	})
	require.Equal(t, 1, len(balances))
	require.Equal(t, bank.CoinBalance{
		Amount: big.NewInt(5000),
		Denom:  "uspendablequery",
	}, bank.CoinBalance(balances[0]))
	require.Empty(t, outputs[1].([]byte))
}

func TestTotalSupplyQuery(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("utotalsupplyquery", sdk.NewInt(123456))))
	require.Nil(t, err)

	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	totalSupply, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).TotalSupplyID)
	require.Nil(t, err)
	args, err := totalSupply.Inputs.Pack([]byte{})
	require.Nil(t, err)
	res, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).TotalSupplyID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := totalSupply.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 2, len(outputs))
	supply := outputs[0].([]struct {
		Amount *big.Int `json:"amount"`
		Denom  string   `json:"denom"`
	})
	found := false
	for _, coin := range supply {
		if coin.Denom == "utotalsupplyquery" {
			found = true
			require.Equal(t, big.NewInt(123456), coin.Amount)
		}
	}
	require.True(t, found)
}

func TestBankParamsQuery(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	params, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).ParamsID)
	require.Nil(t, err)
	res, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, p.GetExecutor().(*bank.PrecompileExecutor).ParamsID, 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := params.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	parsedParams := outputs[0].(struct {
		SendEnabled []struct {
			Denom   string `json:"denom"`
			Enabled bool   `json:"enabled"`
		} `json:"sendEnabled"`
		DefaultSendEnabled bool `json:"defaultSendEnabled"`
	})
	expectedParams := k.BankKeeper().GetParams(ctx)
	require.Equal(t, expectedParams.DefaultSendEnabled, parsedParams.DefaultSendEnabled)
	require.Equal(t, len(expectedParams.SendEnabled), len(parsedParams.SendEnabled))
}

func TestDenomMetadataQuery(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	metadata := banktypes.Metadata{
		Description: "Test denom metadata",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: "udenommetaquery", Exponent: 0, Aliases: []string{"microdenommetaquery"}},
			{Denom: "denommetaquery", Exponent: 6, Aliases: []string{"DENOMMETAQUERY"}},
		},
		Base:    "udenommetaquery",
		Display: "denommetaquery",
		Name:    "Denom Meta Query",
		Symbol:  "DMQ",
	}
	k.BankKeeper().SetDenomMetaData(ctx, metadata)

	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	denomMetadata, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).DenomMetadataID)
	require.Nil(t, err)
	args, err := denomMetadata.Inputs.Pack("udenommetaquery")
	require.Nil(t, err)
	res, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).DenomMetadataID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := denomMetadata.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	parsedMetadata := outputs[0].(struct {
		Description string `json:"description"`
		DenomUnits  []struct {
			Denom    string   `json:"denom"`
			Exponent uint32   `json:"exponent"`
			Aliases  []string `json:"aliases"`
		} `json:"denomUnits"`
		Base    string `json:"base"`
		Display string `json:"display"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
	})
	require.Equal(t, metadata.Description, parsedMetadata.Description)
	require.Equal(t, len(metadata.DenomUnits), len(parsedMetadata.DenomUnits))
	for i, du := range metadata.DenomUnits {
		require.Equal(t, du.Denom, parsedMetadata.DenomUnits[i].Denom)
		require.Equal(t, du.Exponent, parsedMetadata.DenomUnits[i].Exponent)
		require.Equal(t, du.Aliases, parsedMetadata.DenomUnits[i].Aliases)
	}
	require.Equal(t, metadata.Base, parsedMetadata.Base)
	require.Equal(t, metadata.Display, parsedMetadata.Display)
	require.Equal(t, metadata.Name, parsedMetadata.Name)
	require.Equal(t, metadata.Symbol, parsedMetadata.Symbol)

	// querying a denom without metadata should error
	args, err = denomMetadata.Inputs.Pack("unosuchdenommeta")
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).DenomMetadataID, args...), 100000, nil, nil, false, false)
	require.NotNil(t, err)
}

func TestDenomsMetadataQuery(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	metadata := banktypes.Metadata{
		Description: "Test denoms metadata",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: "udenomsmetaquery", Exponent: 0, Aliases: []string{"microdenomsmetaquery"}},
		},
		Base:    "udenomsmetaquery",
		Display: "udenomsmetaquery",
		Name:    "Denoms Meta Query",
		Symbol:  "DSMQ",
	}
	k.BankKeeper().SetDenomMetaData(ctx, metadata)

	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	denomsMetadata, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).DenomsMetadataID)
	require.Nil(t, err)
	args, err := denomsMetadata.Inputs.Pack([]byte{})
	require.Nil(t, err)
	res, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).DenomsMetadataID, args...), 100000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := denomsMetadata.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 2, len(outputs))
	parsedMetadatas := outputs[0].([]struct {
		Description string `json:"description"`
		DenomUnits  []struct {
			Denom    string   `json:"denom"`
			Exponent uint32   `json:"exponent"`
			Aliases  []string `json:"aliases"`
		} `json:"denomUnits"`
		Base    string `json:"base"`
		Display string `json:"display"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
	})
	found := false
	for _, parsedMetadata := range parsedMetadatas {
		if parsedMetadata.Base == "udenomsmetaquery" {
			found = true
			require.Equal(t, metadata.Description, parsedMetadata.Description)
			require.Equal(t, metadata.Display, parsedMetadata.Display)
			require.Equal(t, metadata.Name, parsedMetadata.Name)
			require.Equal(t, metadata.Symbol, parsedMetadata.Symbol)
			require.Equal(t, 1, len(parsedMetadata.DenomUnits))
			require.Equal(t, "udenomsmetaquery", parsedMetadata.DenomUnits[0].Denom)
		}
	}
	require.True(t, found)
	// all metadata records fit in one page, so the next page key should be empty
	require.Empty(t, outputs[1].([]byte))
}

package bank_test

import (
	"embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
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

func TestRun(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

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
	p, err := bank.NewPrecompile(k.BankKeeper(), k, k.AccountKeeper())
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
	_, err = p.Run(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), nil, false) // should error because address is not whitelisted
	require.NotNil(t, err)

	// Precompile sendNative test error
	sendNative, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID)
	require.Nil(t, err)
	seiAddrString := seiAddr.String()
	argsNativeError, err := sendNative.Inputs.Pack(seiAddrString)
	require.Nil(t, err)
	// 0 amount disallowed
	_, err = p.Run(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), big.NewInt(0), false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack("")
	require.Nil(t, err)
	_, err = p.Run(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), big.NewInt(100), false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack("invalidaddr")
	require.Nil(t, err)
	_, err = p.Run(&evm, senderEVMAddr, senderEVMAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), big.NewInt(100), false)
	require.NotNil(t, err)
	argsNativeError, err = sendNative.Inputs.Pack(senderAddr.String())
	require.Nil(t, err)
	_, err = p.Run(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNativeError...), big.NewInt(100), false)
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
	ante.Preprocess(ctx, req)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
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
		banktypes.NewCoinReceivedEvent(senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(172056)))),
		// tip is paid to the validator
		banktypes.NewCoinReceivedEvent(sdk.MustAccAddressFromBech32("sei1v4mx6hmrda5kucnpwdjsqqqqqqqqqqqqlve8dv"), sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(27944)))),
	}

	require.EqualValues(t, expectedEvts.ToABCIEvents(), evts)

	// Use precompile balance to verify sendNative usei amount succeeded
	balance, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID)
	require.Nil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "usei")
	require.Nil(t, err)
	precompileRes, err := p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), nil, false)
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
	_, err = p.Run(&evm, evmAddr, evmAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendNativeID, argsNewAccount...), big.NewInt(1), false)
	require.Nil(t, err)
	// should create account if not exists
	require.NotNil(t, k.AccountKeeper().GetAccount(statedb.Ctx(), newAddr))

	// test get all balances
	allBalances, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID)
	require.Nil(t, err)
	args, err = allBalances.Inputs.Pack(senderEVMAddr)
	require.Nil(t, err)
	precompileRes, err = p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID, args...), nil, false)
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
		Amount: big.NewInt(9972045),
		Denom:  "usei",
	}, bank.CoinBalance(parsedBalances[1]))

	// Verify errors properly raised on bank balance calls with incorrect inputs
	_, err = p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args[:1]...), nil, false)
	require.NotNil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "")
	require.Nil(t, err)
	_, err = p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), nil, false)
	require.NotNil(t, err)

	// invalid input
	_, err = p.Run(&evm, common.Address{}, common.Address{}, []byte{1, 2, 3, 4}, nil, false)
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
	p, err := bank.NewPrecompile(k.BankKeeper(), k, k.AccountKeeper())
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
	_, err = p.Run(&evm, pointerAddr, pointerAddr, append(p.GetExecutor().(*bank.PrecompileExecutor).SendID, args...), nil, false) // should not error
	require.Nil(t, err)

	// Use precompile balance to verify sendNative usei amount succeeded
	balance, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID)
	require.Nil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "ufoo")
	require.Nil(t, err)
	precompileRes, err := p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), nil, false)
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
	precompileRes, err = p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).AllBalancesID, args...), nil, false)
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
	_, err = p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args[:1]...), nil, false)
	require.NotNil(t, err)
	args, err = balance.Inputs.Pack(evmAddr, "")
	require.Nil(t, err)
	_, err = p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID, args...), nil, false)
	require.NotNil(t, err)

	// invalid input
	_, err = p.Run(&evm, common.Address{}, common.Address{}, []byte{1, 2, 3, 4}, nil, false)
	require.NotNil(t, err)
}

func TestMetadata(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	k.BankKeeper().SetDenomMetaData(ctx, banktypes.Metadata{Name: "SEI", Symbol: "usei", Base: "usei"})
	p, err := bank.NewPrecompile(k.BankKeeper(), k, k.AccountKeeper())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	name, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).NameID)
	require.Nil(t, err)
	args, err := name.Inputs.Pack("usei")
	require.Nil(t, err)
	res, err := p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).NameID, args...), nil, false)
	require.Nil(t, err)
	outputs, err := name.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, "SEI", outputs[0])

	symbol, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).SymbolID)
	require.Nil(t, err)
	args, err = symbol.Inputs.Pack("usei")
	require.Nil(t, err)
	res, err = p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).SymbolID, args...), nil, false)
	require.Nil(t, err)
	outputs, err = symbol.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, "usei", outputs[0])

	decimal, err := p.ABI.MethodById(p.GetExecutor().(*bank.PrecompileExecutor).DecimalsID)
	require.Nil(t, err)
	args, err = decimal.Inputs.Pack("usei")
	require.Nil(t, err)
	res, err = p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*bank.PrecompileExecutor).DecimalsID, args...), nil, false)
	require.Nil(t, err)
	outputs, err = decimal.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, uint8(0), outputs[0])
}

func TestRequiredGas(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	p, err := bank.NewPrecompile(k.BankKeeper(), k, k.AccountKeeper())
	require.Nil(t, err)
	balanceRequiredGas := p.RequiredGas(p.GetExecutor().(*bank.PrecompileExecutor).BalanceID)
	require.Equal(t, uint64(1000), balanceRequiredGas)
	// invalid method
	require.Equal(t, uint64(3000), p.RequiredGas([]byte{1, 1, 1, 1}))
}

func TestAddress(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	p, err := bank.NewPrecompile(k.BankKeeper(), k, k.AccountKeeper())
	require.Nil(t, err)
	require.Equal(t, common.HexToAddress(bank.BankAddress), p.Address())
}

package tests

import (
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

func cw20Initializer(mnemonic string, pointer bool) func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		code, err := os.ReadFile("../../contracts/wasm/cw20_base.wasm")
		if err != nil {
			panic(err)
		}
		creator := getSeiAddrWithMnemonic(mnemonic)
		codeID, err := a.EvmKeeper.WasmKeeper().Create(ctx, creator, code, nil)
		if err != nil {
			panic(err)
		}
		contractAddr, _, err := a.EvmKeeper.WasmKeeper().Instantiate(ctx, codeID, creator, creator,
			[]byte(fmt.Sprintf("{\"name\":\"test\",\"symbol\":\"test\",\"decimals\":6,\"initial_balances\":[{\"address\":\"%s\",\"amount\":\"1000000000\"}]}",
				creator.String())), "test", sdk.NewCoins())
		if err != nil {
			panic(err)
		}
		evmAddr := common.BytesToAddress(contractAddr)
		a.EvmKeeper.SetAddressMapping(ctx, contractAddr, evmAddr)

		if pointer {
			blockCtx, err := a.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
			if err != nil {
				panic(err)
			}
			cfg := types.DefaultChainConfig().EthereumConfig(a.EvmKeeper.ChainID(ctx))
			evmInstance := vm.NewEVM(*blockCtx, state.NewDBImpl(ctx, &a.EvmKeeper, false), cfg, vm.Config{}, a.EvmKeeper.CustomPrecompiles(ctx))
			_, err = a.EvmKeeper.UpsertERCCW20Pointer(ctx, evmInstance, contractAddr.String(), utils.ERCMetadata{Name: "test", Symbol: "test"})
			if err != nil {
				panic(err)
			}
		}
	}
}

func cwIterInitializer(mnemonic string) func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		code, err := os.ReadFile("../../example/cosmwasm/iter/artifacts/iter.wasm")
		if err != nil {
			panic(err)
		}
		creator := getSeiAddrWithMnemonic(mnemonic)
		codeID, err := a.EvmKeeper.WasmKeeper().Create(ctx, creator, code, nil)
		if err != nil {
			panic(err)
		}
		contractAddr, _, err := a.EvmKeeper.WasmKeeper().Instantiate(ctx, codeID, creator, creator, []byte("{}"), "test", sdk.NewCoins())
		if err != nil {
			panic(err)
		}
		evmAddr := common.BytesToAddress(contractAddr)
		a.EvmKeeper.SetAddressMapping(ctx, contractAddr, evmAddr)
	}
}

var erc20DeployerMnemonics = "number friend tray advice become blame morning glow final under unlock core employ side mimic local load flag birth hire doctor immense guess net"
var erc20Addr = common.HexToAddress("0x8bFEF0785c95Cb3D4a64202AB283c45ae6c50436") // deterministic with the mnemonic above as the deployer
var mixedLogTesterDeployerMnemonics = "area level during surge alley leader clock hard teach feel evidence tattoo snack betray scare six industry winner false improve various never silent protect"
var mixedLogTesterAddr = common.HexToAddress("0x9023C8C1dB86337278f64c79bDf0aD8402B9b17c") // deterministic with the mnemonic above as the deployer

func erc20Initializer() func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		contractData := GetBin("ERC20")
		evmAddr := getAddrWithMnemonic(erc20DeployerMnemonics)
		seiAddr := getSeiAddrWithMnemonic(erc20DeployerMnemonics)
		tx := ethtypes.NewContractCreation(0, common.Big0, 8000000, common.Big0, contractData)
		protoTx, _ := ethtx.NewLegacyTx(tx)
		msg, _ := types.NewMsgEVMTransaction(protoTx)
		msg.Derived = &derived.Derived{
			SenderEVMAddr: evmAddr,
			SenderSeiAddr: seiAddr,
		}
		mnemonicInitializer(erc20DeployerMnemonics)(ctx, a)
		msgServer := keeper.NewMsgServerImpl(&a.EvmKeeper)
		_, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), msg)
		if err != nil {
			panic(err)
		}
	}
}

func mixedLogTesterInitializer() func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		contractData := GetBin("MixedLogTester")
		parsedABI, _ := abi.JSON(strings.NewReader(string(GetABI("MixedLogTester"))))
		args, _ := parsedABI.Pack("", "sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau")
		evmAddr := getAddrWithMnemonic(mixedLogTesterDeployerMnemonics)
		seiAddr := getSeiAddrWithMnemonic(mixedLogTesterDeployerMnemonics)
		tx := ethtypes.NewContractCreation(0, common.Big0, 5000000, common.Big0, append(contractData, args...))
		protoTx, _ := ethtx.NewLegacyTx(tx)
		msg, _ := types.NewMsgEVMTransaction(protoTx)
		msg.Derived = &derived.Derived{
			SenderEVMAddr: evmAddr,
			SenderSeiAddr: seiAddr,
		}
		mnemonicInitializer(mixedLogTesterDeployerMnemonics)(ctx, a)
		msgServer := keeper.NewMsgServerImpl(&a.EvmKeeper)
		_, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), msg)
		if err != nil {
			panic(err)
		}
		r, _ := a.EvmKeeper.GetTransientReceipt(ctx, tx.Hash(), 0)

		_ = a.EvmKeeper.SetERC20CW20Pointer(ctx,
			"sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau",
			common.HexToAddress(r.ContractAddress))
	}
}

func GetBin(name string) []byte {
	code, err := os.ReadFile(fmt.Sprintf("./%s.bin", name))
	if err != nil {
		panic(fmt.Sprintf("failed to read %s contract binary", name))
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic(fmt.Sprintf("failed to decode %s contract binary", name))
	}
	return bz
}

func GetABI(name string) []byte {
	bz, err := os.ReadFile(fmt.Sprintf("./%s.abi", name))
	if err != nil {
		panic(fmt.Sprintf("failed to read %s contract ABI", name))
	}
	return bz
}

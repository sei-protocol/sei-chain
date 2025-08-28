package tests

import (
	"fmt"
	"os"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/wsei"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

func cw20Initializer(mnemonic string) func(ctx sdk.Context, a *app.App) {
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

func erc20Initializer() func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		contractData := wsei.GetBin()
		evmAddr := getAddrWithMnemonic(erc20DeployerMnemonics)
		seiAddr := getSeiAddrWithMnemonic(erc20DeployerMnemonics)
		tx := ethtypes.NewContractCreation(0, common.Big0, 1000000, common.Big0, contractData)
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

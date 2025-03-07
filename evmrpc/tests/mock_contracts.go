package tests

import (
	"fmt"
	"os"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/app"
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

package processblock

import (
	"os"
	"path/filepath"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

func (a *App) NewContract(admin sdk.AccAddress, source string) sdk.AccAddress {
	source = filepath.Clean(source)
	wasm, err := os.ReadFile(source)
	if err != nil {
		panic(err)
	}
	wasmKeeper := a.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeID, err := contractKeeper.Create(a.Ctx(), admin, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(a.Ctx(), codeID, admin, admin, []byte("{}"), "test", sdk.NewCoins())
	if err != nil {
		panic(err)
	}
	return contractAddr
}

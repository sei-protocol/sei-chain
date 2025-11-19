package processblock

import (
	"os"
	"path/filepath"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (a *App) NewContract(admin seitypes.AccAddress, source string) seitypes.AccAddress {
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

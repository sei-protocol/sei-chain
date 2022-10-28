package wasmbinding

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	EventTypeWasmContractWithIncorrectDependency = "wasm-incorrect-dep"
	AttributeKeyWasmContractAddress              = "contract-addr"
)

func emitIncorrectDependencyWasmEvent(ctx sdk.Context, contractAddr string) {
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(EventTypeWasmContractWithIncorrectDependency, sdk.NewAttribute(AttributeKeyWasmContractAddress, contractAddr)),
	)
}

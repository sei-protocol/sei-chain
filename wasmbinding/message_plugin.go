package wasmbinding

// import (
// 	"encoding/json"

// 	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
// 	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
// 	sdk "github.com/cosmos/cosmos-sdk/types"
// 	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
// 	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
// )

// func CustomMessageDecorator(bank *bankkeeper.BaseKeeper) func(wasmkeeper.Messenger) wasmkeeper.Messenger {
// 	return func(old wasmkeeper.Messenger) wasmkeeper.Messenger {
// 		return &CustomMessenger{
// 			wrapped: old,
// 			bank:    bank,
// 		}
// 	}
// }

// type CustomMessenger struct {
// 	wrapped wasmkeeper.Messenger
// 	bank    *bankkeeper.BaseKeeper
// }

// var _ wasmkeeper.Messenger = (*CustomMessenger)(nil)

// func (m *CustomMessenger) DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg) ([]sdk.Event, [][]byte, error) {
// 	if msg.Custom != nil {
// 		// only handle the happy path where this is really creating / minting / swapping ...
// 		// leave everything else for the wrapped version
// 		var contractMsg bindings.SeiMessage
// 		if err := json.Unmarshal(msg.Custom, &contractMsg); err != nil {
// 			return nil, nil, sdkerrors.Wrap(err, "sei msg")
// 		}
// 	}
// 	return m.wrapped.DispatchMsg(ctx, contractAddr, contractIBCPortID, msg)
// }

// func parseAddress(addr string) (sdk.AccAddress, error) {
// 	parsed, err := sdk.AccAddressFromBech32(addr)
// 	if err != nil {
// 		return nil, sdkerrors.Wrap(err, "address from bech32")
// 	}
// 	err = sdk.VerifyAddressFormat(parsed)
// 	if err != nil {
// 		return nil, sdkerrors.Wrap(err, "verify address format")
// 	}
// 	return parsed, nil
// }

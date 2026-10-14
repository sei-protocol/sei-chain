package wasmbinding

import (
	"encoding/json"
	"strings"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
)

// legacyBurnTokensMsg is the burn_tokens custom message of the retired tokenfactory module.
type legacyBurnTokensMsg struct {
	BurnTokens *struct {
		Amount sdk.Coin `json:"amount"`
	} `json:"burn_tokens"`
}

// LegacyBurnTokensHandler burns coins from the calling contract's own balance for the
// retired tokenfactory burn_tokens custom message. It accepts only denoms the contract
// created, of the form factory/<contract>/<subdenom>, and leaves every other message to
// the next handler.
func LegacyBurnTokensHandler(burner wasmtypes.Burner) wasmkeeper.MessageHandlerFunc {
	return func(ctx sdk.Context, contractAddr sdk.AccAddress, _ string, msg wasmvmtypes.CosmosMsg, _ wasmvmtypes.MessageInfo, _ wasmtypes.CodeInfo) ([]sdk.Event, [][]byte, error) {
		var parsed legacyBurnTokensMsg
		if msg.Custom == nil || json.Unmarshal(msg.Custom, &parsed) != nil || parsed.BurnTokens == nil {
			return nil, nil, wasmtypes.ErrUnknownMsg
		}
		amount := parsed.BurnTokens.Amount
		if err := amount.Validate(); err != nil || !amount.IsPositive() {
			return nil, nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidCoins, "burn_tokens amount %s", amount)
		}
		if !strings.HasPrefix(amount.Denom, "factory/"+contractAddr.String()+"/") {
			return nil, nil, sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s was not created by %s", amount.Denom, contractAddr)
		}
		coins := sdk.NewCoins(amount)
		if err := burner.SendCoinsFromAccountToModule(ctx, contractAddr, wasmtypes.ModuleName, coins); err != nil {
			return nil, nil, sdkerrors.Wrap(err, "transfer to module")
		}
		if err := burner.BurnCoins(ctx, wasmtypes.ModuleName, coins); err != nil {
			return nil, nil, sdkerrors.Wrap(err, "burn coins")
		}
		return nil, nil, nil
	}
}

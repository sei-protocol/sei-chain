package wasmbinding

import (
	"encoding/json"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	evmwasm "github.com/sei-protocol/sei-chain/x/evm/client/wasm"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
)

type SeiWasmMessage struct {
	CreateDenom     json.RawMessage `json:"create_denom,omitempty"`
	MintTokens      json.RawMessage `json:"mint_tokens,omitempty"`
	BurnTokens      json.RawMessage `json:"burn_tokens,omitempty"`
	ChangeAdmin     json.RawMessage `json:"change_admin,omitempty"`
	SetMetadata     json.RawMessage `json:"set_metadata,omitempty"`
	CallEVM         json.RawMessage `json:"call_evm,omitempty"`
	DelegateCallEVM json.RawMessage `json:"delegate_call_evm,omitempty"`
}

func CustomEncoder(sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo, codeInfo wasmtypes.CodeInfo) ([]sdk.Msg, error) {
	var parsedMessage SeiWasmMessage
	if err := json.Unmarshal(msg, &parsedMessage); err != nil {
		return []sdk.Msg{}, sdkerrors.Wrap(err, "Error parsing Sei Wasm Message")
	}
	switch {
	case parsedMessage.CreateDenom != nil:
		return tokenfactorywasm.EncodeTokenFactoryCreateDenom(parsedMessage.CreateDenom, sender)
	case parsedMessage.MintTokens != nil:
		return tokenfactorywasm.EncodeTokenFactoryMint(parsedMessage.MintTokens, sender)
	case parsedMessage.BurnTokens != nil:
		return tokenfactorywasm.EncodeTokenFactoryBurn(parsedMessage.BurnTokens, sender)
	case parsedMessage.ChangeAdmin != nil:
		return tokenfactorywasm.EncodeTokenFactoryChangeAdmin(parsedMessage.ChangeAdmin, sender)
	case parsedMessage.SetMetadata != nil:
		return tokenfactorywasm.EncodeTokenFactorySetMetadata(parsedMessage.SetMetadata, sender)
	case parsedMessage.CallEVM != nil:
		return evmwasm.EncodeCallEVM(parsedMessage.CallEVM, sender, info)
	case parsedMessage.DelegateCallEVM != nil:
		return evmwasm.EncodeDelegateCallEVM(parsedMessage.DelegateCallEVM, sender, info, codeInfo)
	default:
		return []sdk.Msg{}, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Wasm Message"}
	}
}

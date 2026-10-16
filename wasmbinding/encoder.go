package wasmbinding

import (
	"encoding/json"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	evmwasm "github.com/sei-protocol/sei-chain/x/evm/client/wasm"
)

type SeiWasmMessage struct {
	CallEVM         json.RawMessage `json:"call_evm,omitempty"`
	DelegateCallEVM json.RawMessage `json:"delegate_call_evm,omitempty"`
}

func CustomEncoder(sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo, codeInfo wasmtypes.CodeInfo) ([]sdk.Msg, error) {
	var parsedMessage SeiWasmMessage
	if err := json.Unmarshal(msg, &parsedMessage); err != nil {
		return []sdk.Msg{}, sdkerrors.Wrap(err, "Error parsing Sei Wasm Message")
	}
	switch {
	case parsedMessage.CallEVM != nil:
		return evmwasm.EncodeCallEVM(parsedMessage.CallEVM, sender, info)
	case parsedMessage.DelegateCallEVM != nil:
		return evmwasm.EncodeDelegateCallEVM(parsedMessage.DelegateCallEVM, sender, info, codeInfo)
	default:
		return []sdk.Msg{}, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Wasm Message"}
	}
}

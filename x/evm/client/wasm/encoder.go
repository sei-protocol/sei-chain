package wasm

import (
	"encoding/base64"
	"encoding/json"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/wasmbinding/bindings"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func EncodeCallEVM(rawMsg json.RawMessage, sender seitypes.AccAddress, info wasmvmtypes.MessageInfo) ([]seitypes.Msg, error) {
	encodedCallEVM := bindings.CallEVM{}
	if err := json.Unmarshal(rawMsg, &encodedCallEVM); err != nil {
		return []seitypes.Msg{}, err
	}
	decodedData, err := base64.StdEncoding.DecodeString(encodedCallEVM.Data)
	if err != nil {
		return []seitypes.Msg{}, err
	}
	internalCallEVMMsg := types.MsgInternalEVMCall{
		Sender: sender.String(),
		To:     encodedCallEVM.To,
		Value:  encodedCallEVM.Value,
		Data:   decodedData,
	}
	return []seitypes.Msg{&internalCallEVMMsg}, nil
}

func EncodeDelegateCallEVM(rawMsg json.RawMessage, sender seitypes.AccAddress, info wasmvmtypes.MessageInfo, codeInfo wasmtypes.CodeInfo) ([]seitypes.Msg, error) {
	encodedCallEVM := bindings.DelegateCallEVM{}
	if err := json.Unmarshal(rawMsg, &encodedCallEVM); err != nil {
		return []seitypes.Msg{}, err
	}
	decodedData, err := base64.StdEncoding.DecodeString(encodedCallEVM.Data)
	if err != nil {
		return []seitypes.Msg{}, err
	}
	s := sender
	if origSender, err := seitypes.AccAddressFromBech32(info.Sender); err == nil {
		s = origSender
	}
	internalCallEVMMsg := types.MsgInternalEVMDelegateCall{
		Sender:       s.String(),
		To:           encodedCallEVM.To,
		CodeHash:     codeInfo.CodeHash,
		Data:         decodedData,
		FromContract: sender.String(),
	}
	return []seitypes.Msg{&internalCallEVMMsg}, nil
}

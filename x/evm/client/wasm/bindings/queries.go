package bindings

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type SeiEVMQuery struct {
	StaticCall           *StaticCallRequest           `json:"static_call,omitempty"`
	ERC20TransferPayload *ERC20TransferPayloadRequest `json:"erc20_transfer_payload,omitempty"`
}

type StaticCallRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
	Data []byte `json:"data"`
}

type ERC20TransferPayloadRequest struct {
	Recipient string   `json:"recipient"`
	Amount    *sdk.Int `json:"amount"`
}

type ERC20TransferPayloadResponse struct {
	EncodedPayload string `json:"encoded_payload"`
}

package bindings

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type SeiEVMQuery struct {
	StaticCall                  *StaticCallRequest                  `json:"static_call,omitempty"`
	ERC20TransferPayload        *ERC20TransferPayloadRequest        `json:"erc20_transfer_payload,omitempty"`
	ERC721Owner                 *ERC721OwnerRequest                 `json:"erc721_owner,omitempty"`
	ERC721TransferPayload       *ERC721TransferPayloadRequest       `json:"erc721_transfer_payload,omitempty"`
	ERC721ApprovePayload        *ERC721ApprovePayloadRequest        `json:"erc721_approve_payload,omitempty"`
	ERC721SetApprovalAllPayload *ERC721SetApprovalAllPayloadRequest `json:"erc721_set_approval_all_payload,omitempty"`
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

type ERC721OwnerRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
}

type ERC721TransferPayloadRequest struct {
	From      string `json:"from"`
	Recipient string `json:"recipient"`
	TokenID   string `json:"token_id"`
}

type ERC721ApprovePayloadRequest struct {
	Spender string `json:"spender"`
	TokenID string `json:"token_id"`
}

type ERC721SetApprovalAllPayloadRequest struct {
	To       string `json:"to"`
	Approved bool   `json:"approved"`
}

type ERCPayloadResponse struct {
	EncodedPayload string `json:"encoded_payload"`
}

type ERC721OwnerResponse struct {
	Owner string `json:"owner"`
}

package bindings

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type SeiEVMQuery struct {
	StaticCall                  *StaticCallRequest                  `json:"static_call,omitempty"`
	ERC20TransferPayload        *ERC20TransferPayloadRequest        `json:"erc20_transfer_payload,omitempty"`
	ERC20TransferFromPayload    *ERC20TransferFromPayloadRequest    `json:"erc20_transfer_from_payload,omitempty"`
	ERC20ApprovePayload         *ERC20ApprovePayloadRequest         `json:"erc20_approve_payload,omitempty"`
	ERC20Allowance              *ERC20AllowanceRequest              `json:"erc20_allowance,omitempty"`
	ERC20TokenInfo              *ERC20TokenInfoRequest              `json:"erc20_token_info,omitempty"`
	ERC20Balance                *ERC20BalanceRequest                `json:"erc20_balance,omitempty"`
	ERC721Owner                 *ERC721OwnerRequest                 `json:"erc721_owner,omitempty"`
	ERC721TransferPayload       *ERC721TransferPayloadRequest       `json:"erc721_transfer_payload,omitempty"`
	ERC721ApprovePayload        *ERC721ApprovePayloadRequest        `json:"erc721_approve_payload,omitempty"`
	ERC721SetApprovalAllPayload *ERC721SetApprovalAllPayloadRequest `json:"erc721_set_approval_all_payload,omitempty"`
	ERC721Approved              *ERC721ApprovedRequest              `json:"erc721_approved,omitempty"`
	ERC721IsApprovedForAll      *ERC721IsApprovedForAllRequest      `json:"erc721_is_approved_for_all,omitempty"`
	ERC721NameSymbol            *ERC721NameSymbolRequest            `json:"erc721_name_symbol,omitempty"`
	ERC721Uri                   *ERC721UriRequest                   `json:"erc721_uri,omitempty"`
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

type ERC20TransferFromPayloadRequest struct {
	Owner     string   `json:"owner"`
	Recipient string   `json:"recipient"`
	Amount    *sdk.Int `json:"amount"`
}

type ERC20ApprovePayloadRequest struct {
	Spender string   `json:"spender"`
	Amount  *sdk.Int `json:"token_id"`
}

type ERC20AllowanceRequest struct {
	ContractAddress string `json:"contract_address"`
	Owner           string `json:"owner"`
	Spender         string `json:"spender"`
}

type ERC20TokenInfoRequest struct {
	ContractAddress string `json:"contract_address"`
	Caller          string `json:"caller"`
}

type ERC20BalanceRequest struct {
	ContractAddress string `json:"contract_address"`
	Account         string `json:"account"`
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

type ERC721ApprovedRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
}

type ERC721IsApprovedForAllRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	Owner           string `json:"owner"`
	Operator        string `json:"operator"`
}

type ERC721NameSymbolRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
}

type ERC721UriRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
}

type ERCPayloadResponse struct {
	EncodedPayload string `json:"encoded_payload"`
}

type ERC20AllowanceResponse struct {
	Allowance *sdk.Int `json:"allowance"`
}

type ERC721OwnerResponse struct {
	Owner string `json:"owner"`
}

type ERC20TokenInfoResponse struct {
	Name        string   `json:"name"`
	Symbol      string   `json:"symbol"`
	Decimal     byte     `json:"decimal"`
	TotalSupply *sdk.Int `json:"total_supply"`
}

type ERC20BalanceResponse struct {
	Balance *sdk.Int `json:"balance"`
}

type ERC721ApprovedResponse struct {
	Approved string `json:"approved"`
}

type ERC721IsApprovedForAllResponse struct {
	IsApproved bool `json:"is_approved"`
}

type ERC721NameSymbolResponse struct {
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
}

type ERC721UriResponse struct {
	Uri string `json:"uri"`
}

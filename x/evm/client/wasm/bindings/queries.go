package bindings

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type SeiEVMQuery struct {
	StaticCall                   *StaticCallRequest                   `json:"static_call,omitempty"`
	ERC20TransferPayload         *ERC20TransferPayloadRequest         `json:"erc20_transfer_payload,omitempty"`
	ERC20TransferFromPayload     *ERC20TransferFromPayloadRequest     `json:"erc20_transfer_from_payload,omitempty"`
	ERC20ApprovePayload          *ERC20ApprovePayloadRequest          `json:"erc20_approve_payload,omitempty"`
	ERC20Allowance               *ERC20AllowanceRequest               `json:"erc20_allowance,omitempty"`
	ERC20TokenInfo               *ERC20TokenInfoRequest               `json:"erc20_token_info,omitempty"`
	ERC20Balance                 *ERC20BalanceRequest                 `json:"erc20_balance,omitempty"`
	ERC721Owner                  *ERC721OwnerRequest                  `json:"erc721_owner,omitempty"`
	ERC721TransferPayload        *ERC721TransferPayloadRequest        `json:"erc721_transfer_payload,omitempty"`
	ERC721ApprovePayload         *ERC721ApprovePayloadRequest         `json:"erc721_approve_payload,omitempty"`
	ERC721SetApprovalAllPayload  *ERC721SetApprovalAllPayloadRequest  `json:"erc721_set_approval_all_payload,omitempty"`
	ERC721Approved               *ERC721ApprovedRequest               `json:"erc721_approved,omitempty"`
	ERC721IsApprovedForAll       *ERC721IsApprovedForAllRequest       `json:"erc721_is_approved_for_all,omitempty"`
	ERC721TotalSupply            *ERC721TotalSupplyRequest            `json:"erc721_total_supply,omitempty"`
	ERC721NameSymbol             *ERC721NameSymbolRequest             `json:"erc721_name_symbol,omitempty"`
	ERC721Uri                    *ERC721UriRequest                    `json:"erc721_uri,omitempty"`
	ERC721RoyaltyInfo            *ERC721RoyaltyInfoRequest            `json:"erc721_royalty_info,omitempty"`
	ERC1155Owner                 *ERC1155OwnerRequest                 `json:"erc1155_owner,omitempty"`
	ERC1155SendBatchPayload      *ERC1155TransferPayloadRequest       `json:"erc1155_transfer_payload,omitempty"`
	ERC1155ApprovePayload        *ERC1155ApprovePayloadRequest        `json:"erc1155_approve_payload,omitempty"`
	ERC1155SetApprovalAllPayload *ERC1155SetApprovalAllPayloadRequest `json:"erc1155_set_approval_all_payload,omitempty"`
	ERC1155Approved              *ERC1155ApprovedRequest              `json:"erc1155_approved,omitempty"`
	ERC1155IsApprovedForAll      *ERC1155IsApprovedForAllRequest      `json:"erc1155_is_approved_for_all,omitempty"`
	ERC1155TotalSupply           *ERC1155TotalSupplyRequest           `json:"erc1155_total_supply,omitempty"`
	ERC1155NameSymbol            *ERC1155NameSymbolRequest            `json:"erc1155_name_symbol,omitempty"`
	ERC1155Uri                   *ERC1155UriRequest                   `json:"erc1155_uri,omitempty"`
	ERC1155RoyaltyInfo           *ERC1155RoyaltyInfoRequest           `json:"erc1155_royalty_info,omitempty"`
	GetEvmAddress                *GetEvmAddressRequest                `json:"get_evm_address,omitempty"`
	GetSeiAddress                *GetSeiAddressRequest                `json:"get_sei_address,omitempty"`
	SupportsInterface            *SupportsInterfaceRequest            `json:"supports_interface,omitempty"`
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
	Amount  *sdk.Int `json:"amount"`
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

type ERC721TotalSupplyRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
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

type ERC721RoyaltyInfoRequest struct {
	Caller          string   `json:"caller"`
	ContractAddress string   `json:"contract_address"`
	TokenID         string   `json:"token_id"`
	SalePrice       *sdk.Int `json:"sale_price"`
}

type GetEvmAddressRequest struct {
	SeiAddress string `json:"sei_address"`
}

type GetSeiAddressRequest struct {
	EvmAddress string `json:"evm_address"`
}

type SupportsInterfaceRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	InterfaceID     string `json:"interface_id"`
}

type StaticCallResponse struct {
	EncodedData string `json:"encoded_data"`
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
	Decimals    byte     `json:"decimals"`
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

type ERC721TotalSupplyResponse struct {
	Supply *sdk.Int `json:"supply"`
}

type ERC721NameSymbolResponse struct {
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
}

type ERC721UriResponse struct {
	Uri string `json:"uri"`
}

type ERC721RoyaltyInfoResponse struct {
	Receiver      string   `json:"receiver"`
	RoyaltyAmount *sdk.Int `json:"royalty_amount"`
}

type GetEvmAddressResponse struct {
	EvmAddress string `json:"evm_address"`
	Associated bool   `json:"associated"`
}

type GetSeiAddressResponse struct {
	SeiAddress string `json:"sei_address"`
	Associated bool   `json:"associated"`
}

type SupportsInterfaceResponse struct {
	Supported bool `json:"supported"`
}

package bindings

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type EVMQueryType string

const (
	StaticCallType        EVMQueryType = "evm_query_static_call"
	ERC20TransferType     EVMQueryType = "evm_query_erc20_transfer"
	ERC20TransferFromType EVMQueryType = "evm_query_erc20_transfer_from"
	ERC20ApproveType      EVMQueryType = "evm_query_erc20_approve"
	ERC20AllowanceType    EVMQueryType = "evm_query_erc20_allowance"
	// #nosec G101 -- the word Token triggers the credential detection
	ERC20TokenInfoType          EVMQueryType = "evm_query_erc20_token_info"
	ERC20BalanceType            EVMQueryType = "evm_query_erc20_balance"
	ERC721OwnerType             EVMQueryType = "evm_query_erc721_owner"
	ERC721TransferType          EVMQueryType = "evm_query_erc721_transfer"
	ERC721ApproveType           EVMQueryType = "evm_query_erc721_approve"
	ERC721SetApprovalAllType    EVMQueryType = "evm_query_erc721_set_approval_all"
	ERC721ApprovedType          EVMQueryType = "evm_query_erc721_approved"
	ERC721IsApprovedForAllType  EVMQueryType = "evm_query_erc721_is_approved_for_all"
	ERC721TotalSupplyType       EVMQueryType = "evm_query_erc721_total_supply"
	ERC721NameSymbolType        EVMQueryType = "evm_query_erc721_name_symbol"
	ERC721UriType               EVMQueryType = "evm_query_erc721_uri"
	ERC721RoyaltyInfoType       EVMQueryType = "evm_query_erc721_royalty_info"
	ERC1155TransferType         EVMQueryType = "evm_query_erc1155_transfer"
	ERC1155BatchTransferType    EVMQueryType = "evm_query_erc1155_batch_transfer"
	ERC1155SetApprovalAllType   EVMQueryType = "evm_query_erc1155_set_approval_all"
	ERC1155IsApprovedForAllType EVMQueryType = "evm_query_erc1155_is_approved_for_all"
	ERC1155BalanceOfType        EVMQueryType = "evm_query_erc1155_balance_of"
	ERC1155BalanceOfBatchType   EVMQueryType = "evm_query_erc1155_balance_of_batch"
	ERC1155TotalSupplyType      EVMQueryType = "evm_query_erc1155_total_supply"
	// #nosec G101 -- the word Token triggers the credential detection
	ERC1155TotalSupplyForTokenType EVMQueryType = "evm_query_erc1155_total_supply_for_token"
	// #nosec G101 -- the word Token triggers the credential detection
	ERC1155TokenExistsType EVMQueryType = "evm_query_erc1155_token_exists"
	ERC1155NameSymbolType  EVMQueryType = "evm_query_erc1155_name_symbol"
	ERC1155UriType         EVMQueryType = "evm_query_erc1155_uri"
	ERC1155RoyaltyInfoType EVMQueryType = "evm_query_erc1155_royalty_info"
	GetEvmAddressType      EVMQueryType = "evm_query_get_evm_address"
	GetSeiAddressType      EVMQueryType = "evm_query_get_sei_address"
	SupportsInterfaceType  EVMQueryType = "evm_query_supports_interface"
)

func (q *SeiEVMQuery) GetQueryType() EVMQueryType {
	if q.StaticCall != nil {
		return StaticCallType
	}
	if q.ERC20TransferPayload != nil {
		return ERC20TransferType
	}
	if q.ERC20TransferFromPayload != nil {
		return ERC20TransferFromType
	}
	if q.ERC20ApprovePayload != nil {
		return ERC20ApproveType
	}
	if q.ERC20Allowance != nil {
		return ERC20AllowanceType
	}
	if q.ERC20TokenInfo != nil {
		return ERC20TokenInfoType
	}
	if q.ERC20Balance != nil {
		return ERC20BalanceType
	}
	if q.ERC721Owner != nil {
		return ERC721OwnerType
	}
	if q.ERC721TransferPayload != nil {
		return ERC721TransferType
	}
	if q.ERC721ApprovePayload != nil {
		return ERC721ApproveType
	}
	if q.ERC721SetApprovalAllPayload != nil {
		return ERC721SetApprovalAllType
	}
	if q.ERC721Approved != nil {
		return ERC721ApprovedType
	}
	if q.ERC721IsApprovedForAll != nil {
		return ERC721IsApprovedForAllType
	}
	if q.ERC721TotalSupply != nil {
		return ERC721TotalSupplyType
	}
	if q.ERC721NameSymbol != nil {
		return ERC721NameSymbolType
	}
	if q.ERC721Uri != nil {
		return ERC721UriType
	}
	if q.ERC721RoyaltyInfo != nil {
		return ERC721RoyaltyInfoType
	}
	if q.ERC1155TransferPayload != nil {
		return ERC1155TransferType
	}
	if q.ERC1155BatchTransferPayload != nil {
		return ERC1155BatchTransferType
	}
	if q.ERC1155SetApprovalAllPayload != nil {
		return ERC1155SetApprovalAllType
	}
	if q.ERC1155IsApprovedForAll != nil {
		return ERC1155IsApprovedForAllType
	}
	if q.ERC1155BalanceOf != nil {
		return ERC1155BalanceOfType
	}
	if q.ERC1155BalanceOfBatch != nil {
		return ERC1155BalanceOfBatchType
	}
	if q.ERC1155TotalSupply != nil {
		return ERC1155TotalSupplyType
	}
	if q.ERC1155TotalSupplyForToken != nil {
		return ERC1155TotalSupplyForTokenType
	}
	if q.ERC1155TokenExists != nil {
		return ERC1155TokenExistsType
	}
	if q.ERC1155NameSymbol != nil {
		return ERC1155NameSymbolType
	}
	if q.ERC1155Uri != nil {
		return ERC1155UriType
	}
	if q.ERC1155RoyaltyInfo != nil {
		return ERC1155RoyaltyInfoType
	}
	if q.GetEvmAddress != nil {
		return GetEvmAddressType
	}
	if q.GetSeiAddress != nil {
		return GetSeiAddressType
	}
	if q.SupportsInterface != nil {
		return SupportsInterfaceType
	}
	return ""
}

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
	ERC1155TransferPayload       *ERC1155TransferPayloadRequest       `json:"erc1155_transfer_payload,omitempty"`
	ERC1155BatchTransferPayload  *ERC1155BatchTransferPayloadRequest  `json:"erc1155_batch_transfer_payload,omitempty"`
	ERC1155SetApprovalAllPayload *ERC1155SetApprovalAllPayloadRequest `json:"erc1155_set_approval_all_payload,omitempty"`
	ERC1155IsApprovedForAll      *ERC1155IsApprovedForAllRequest      `json:"erc1155_is_approved_for_all,omitempty"`
	ERC1155BalanceOf             *ERC1155BalanceOfRequest             `json:"erc1155_balance_of,omitempty"`
	ERC1155BalanceOfBatch        *ERC1155BalanceOfBatchRequest        `json:"erc1155_balance_of_batch,omitempty"`
	ERC1155TotalSupply           *ERC1155TotalSupplyRequest           `json:"erc1155_total_supply,omitempty"`
	ERC1155TotalSupplyForToken   *ERC1155TotalSupplyForTokenRequest   `json:"erc1155_total_supply_for_token,omitempty"`
	ERC1155TokenExists           *ERC1155TokenExistsRequest           `json:"erc1155_token_exists,omitempty"`
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

type ERC1155TransferPayloadRequest struct {
	From      string   `json:"from"`
	Recipient string   `json:"recipient"`
	TokenID   string   `json:"token_id"`
	Amount    *sdk.Int `json:"amount"`
}

type ERC1155BatchTransferPayloadRequest struct {
	From      string     `json:"from"`
	Recipient string     `json:"recipient"`
	TokenIDs  []string   `json:"token_ids"`
	Amounts   []*sdk.Int `json:"amounts"`
}

type ERC1155SetApprovalAllPayloadRequest struct {
	To       string `json:"to"`
	Approved bool   `json:"approved"`
}

type ERC1155IsApprovedForAllRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	Owner           string `json:"owner"`
	Operator        string `json:"operator"`
}

type ERC1155BalanceOfRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	Account         string `json:"account"`
	TokenID         string `json:"token_id"`
}

type ERC1155BalanceOfBatchRequest struct {
	Caller          string   `json:"caller"`
	ContractAddress string   `json:"contract_address"`
	Accounts        []string `json:"accounts"`
	TokenIDs        []string `json:"token_ids"`
}

type ERC1155TotalSupplyRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
}

type ERC1155TotalSupplyForTokenRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
}

type ERC1155TokenExistsRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
}

type ERC1155NameSymbolRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
}

type ERC1155UriRequest struct {
	Caller          string `json:"caller"`
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
}

type ERC1155RoyaltyInfoRequest struct {
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

type ERC1155IsApprovedForAllResponse struct {
	IsApproved bool `json:"is_approved"`
}

type ERC1155BalanceOfResponse struct {
	Balance *sdk.Int `json:"balance"`
}

type ERC1155BalanceOfBatchResponse struct {
	Balances []*sdk.Int `json:"balances"`
}

type ERC1155TotalSupplyResponse struct {
	Supply *sdk.Int `json:"supply"`
}

type ERC1155TotalSupplyForTokenResponse struct {
	Supply *sdk.Int `json:"supply"`
}

type ERC1155TokenExistsResponse struct {
	Exists bool `json:"exists"`
}

type ERC1155NameSymbolResponse struct {
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
}

type ERC1155UriResponse struct {
	Uri string `json:"uri"`
}

type ERC1155RoyaltyInfoResponse struct {
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

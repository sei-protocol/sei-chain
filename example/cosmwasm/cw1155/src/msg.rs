use cosmwasm_std::{CosmosMsg, CustomMsg, CustomQuery, Uint128};
use schemars::JsonSchema;
use cosmwasm_schema::{cw_serde, QueryResponses};
use cw1155::Cw1155QueryMsg;
use serde::{Deserialize, Serialize};

#[cw_serde]
pub struct InstantiateMsg {
    pub erc1155_address: String,
}

pub type QueryMsg = Cw1155QueryMsg<CwErc1155QueryMsg>;

/// SeiRoute is enum type to represent sei query route path
#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Route {
    Evm,
}

/// EvmQueryWrapper is an override of QueryRequest::Custom to access EVM
#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub struct EvmQueryWrapper {
    pub route: Route,
    pub query_data: EvmQuery,
}

// implement custom query
impl CustomQuery for EvmQueryWrapper {}

/// EvmQuery is defines available query datas
#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum EvmQuery {
    Erc1155TransferSinglePayload {
        from: String,
        recipient: String,
        token_id: String,
        amount: Uint128,
    },
    Erc1155TransferBatchPayload {
        from: String,
        recipient: String,
        token_ids: Vec<String>,
        amounts: Vec<Uint128>,
    },
    // todo - is this implemented in erc1155?
    Erc1155ApprovePayload {
        spender: String,
        token_id: String,
    },
    Erc1155BalanceOf {
        caller: String,
        contract_address: String,
        account: String,
        token_id: String,
    },
    Erc1155BalanceOfBatch {
        caller: String,
        contract_address: String,
        accounts: Vec<String>,
        token_ids: Vec<String>,
    },
    // todo - is this implemented in erc1155?
    Erc1155Approved {
        caller: String,
        contract_address: String,
        token_id: String,
    },
    Erc1155IsApprovedForAll {
        caller: String,
        contract_address: String,
        owner: String,
        operator: String,
    },
    Erc1155SetApprovalAllPayload {
        to: String,
        approved: bool,
    },
    Erc1155NameSymbol {
        caller: String,
        contract_address: String,
    },
    Erc1155Uri {
        caller: String,
        contract_address: String,
        token_id: String,
    },
    // todo - quantity field?
    Erc1155RoyaltyInfo {
        caller: String,
        contract_address: String,
        token_id: String,
        sale_price: Uint128,
    },
    SupportsInterface {
        caller: String,
        contract_address: String,
        interface_id: String,
    },
    Erc1155TotalSupply {
        caller: String,
        contract_address: String,
    },
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct ErcPayloadResponse {
    pub encoded_payload: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc1155BalanceOfResponse {
    pub amount: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc1155IsApprovedForAllResponse {
    pub is_approved: bool,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc1155NameSymbolResponse {
    pub name: String,
    pub symbol: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc1155UriResponse {
    pub uri: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc1155RoyaltyInfoResponse {
    pub receiver: String,
    pub royalty_amount: Uint128,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct SupportsInterfaceResponse {
    pub supported: bool,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc1155TotalSupplyResponse {
    pub supply: Uint128,
}

// implement custom query
impl CustomMsg for EvmMsg {}

// this is a helper to be able to return these as CosmosMsg easier
impl From<EvmMsg> for CosmosMsg<EvmMsg> {
    fn from(original: EvmMsg) -> Self {
        CosmosMsg::Custom(original)
    }
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum EvmMsg {
    DelegateCallEvm {
        to: String,
        data: String, // base64 encoded
    },
}

#[cw_serde]
#[derive(QueryResponses)]
pub enum CwErc1155QueryMsg {
    #[returns(String)]
    EvmAddress {},

    // cw2981
    /// Should be called on sale to see if royalties are owed
    /// by the marketplace selling the NFT, if CheckRoyalties
    /// returns true
    /// See https://eips.ethereum.org/EIPS/eip-2981
    #[returns(cw2981_royalties::msg::RoyaltiesInfoResponse)]
    RoyaltyInfo {
        token_id: String,
        // the denom of this sale must also be the denom returned by RoyaltiesInfoResponse
        // this was originally implemented as a Coin
        // however that would mean you couldn't buy using CW20s
        // as CW20 is just mapping of addr -> balance
        sale_price: Uint128,
    },
    /// Called against contract to determine if this NFT
    /// implements royalties. Should return a boolean as part of
    /// CheckRoyaltiesResponse - default can simply be true
    /// if royalties are implemented at token level
    /// (i.e. always check on sale)
    #[returns(cw2981_royalties::msg::CheckRoyaltiesResponse)]
    CheckRoyalties {},
}

impl Default for CwErc1155QueryMsg {
    fn default() -> Self {
        CwErc1155QueryMsg::EvmAddress {}
    }
}

impl CustomMsg for CwErc1155QueryMsg {}
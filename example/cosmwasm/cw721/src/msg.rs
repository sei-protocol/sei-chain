use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::{CosmosMsg, CustomMsg, CustomQuery, Uint128};
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

pub use cw721_base::{ExecuteMsg, QueryMsg};

#[cw_serde]
pub struct InstantiateMsg {
    pub erc721_address: String,
}

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
    Erc721TransferPayload {
        from: String,
        recipient: String,
        token_id: String,
    },
    Erc721ApprovePayload {
        spender: String,
        token_id: String,
    },
    Erc721Owner {
        caller: String,
        contract_address: String,
        token_id: String,
    },
    Erc721Approved {
        caller: String,
        contract_address: String,
        token_id: String,
    },
    Erc721IsApprovedForAll {
        caller: String,
        contract_address: String,
        owner: String,
        operator: String,
    },
    Erc721SetApprovalAllPayload {
        to: String,
        approved: bool,
    },
    Erc721TotalSupply {
        caller: String,
        contract_address: String,
    },
    Erc721NameSymbol {
        caller: String,
        contract_address: String,
    },
    Erc721Uri {
        caller: String,
        contract_address: String,
        token_id: String,
    },
    Erc721RoyaltyInfo {
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
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct ErcPayloadResponse {
    pub encoded_payload: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc721OwnerResponse {
    pub owner: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc721ApprovedResponse {
    pub approved: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc721IsApprovedForAllResponse {
    pub is_approved: bool,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc721TotalSupplyResponse {
    pub supply: Uint128,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc721NameSymbolResponse {
    pub name: String,
    pub symbol: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc721UriResponse {
    pub uri: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc721RoyaltyInfoResponse {
    pub receiver: String,
    pub royalty_amount: Uint128,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct SupportsInterfaceResponse {
    pub supported: bool,
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
pub enum CwErc721QueryMsg {
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

impl Default for CwErc721QueryMsg {
    fn default() -> Self {
        CwErc721QueryMsg::EvmAddress {}
    }
}

impl CustomMsg for CwErc721QueryMsg {}

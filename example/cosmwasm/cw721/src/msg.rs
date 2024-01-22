use cosmwasm_std::{CosmosMsg, CustomMsg, CustomQuery};
use schemars::JsonSchema;
use cosmwasm_schema::cw_serde;
use serde::{Deserialize, Serialize};

pub use cw721::Cw721ExecuteMsg as ExecuteMsg;

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
    Erc721SetApprovalAllPayload {
        to: String,
        approved: bool,
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
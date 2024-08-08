use cosmwasm_std::{CosmosMsg, CustomMsg, CustomQuery, StdResult, Uint128, WasmMsg};
use cw20::Cw20ReceiveMsg;
use schemars::JsonSchema;
use cosmwasm_schema::cw_serde;
use serde::{Deserialize, Serialize};

pub use cw20::{Cw20ExecuteMsg as ExecuteMsg, Cw20QueryMsg as QueryMsg};

#[cw_serde]
pub struct InstantiateMsg {
    pub erc20_address: String,
}

#[cw_serde]
pub struct MigrateMsg {}

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
    Erc20TransferPayload {
        recipient: String,
        amount: Uint128,
    },
    Erc20TransferFromPayload {
        owner: String,
        recipient: String,
        amount: Uint128,
    },
    Erc20ApprovePayload {
        spender: String,
        amount: Uint128,
    },
    Erc20Allowance {
        contract_address: String,
        owner: String,
        spender: String,
    },
    Erc20TokenInfo {
        contract_address: String,
        caller: String,
    },
    Erc20Balance {
        contract_address: String,
        account: String,
    },
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct ErcPayloadResponse {
    pub encoded_payload: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct Erc20AllowanceResponse {
    pub allowance: Uint128,
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

/// Helper to convert a Cw20ReceiveMsg into an EvmMsg
pub fn cw20receive_into_cosmos_msg<T: Into<String>, C>(contract_addr: T, message: Cw20ReceiveMsg) -> StdResult<CosmosMsg<C>> 
where
    C: Clone + std::fmt::Debug + PartialEq + JsonSchema,
{
    let msg = message.into_binary()?;
    let execute = WasmMsg::Execute {
        contract_addr: contract_addr.into(),
        msg,
        funds: vec![],
    };

    Ok(execute.into())
}
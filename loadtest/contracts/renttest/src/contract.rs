use cosmwasm_std::{
    entry_point, DepsMut, Env, MessageInfo,
    Response, StdResult,
};
use cosmwasm_schema::{cw_serde};
use cw_storage_plus::{Map};

#[entry_point]
pub fn instantiate(
    _deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    _msg: InstantiateMsg,
) -> StdResult<Response> {
    Ok(Response::new())
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: ExecuteMsg,
) -> StdResult<Response> {
    match msg {
        ExecuteMsg::Add { key, value } => {
            STATE.save(deps.storage, key, &value)?;
            Ok(Response::new())
        }
        ExecuteMsg::Delete { key } => {
            STATE.remove(deps.storage, key);
            Ok(Response::new())
        }
    }
}

#[cw_serde]
pub struct InstantiateMsg {}

#[cw_serde]
pub enum ExecuteMsg {
    Add {
        key: String,
        value: String,
    },
    Delete {
        key: String,
    },
}

pub const STATE: Map<String, String> = Map::new("state");
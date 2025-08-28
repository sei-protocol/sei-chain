#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    DepsMut, Env, MessageInfo, Response, StdError, to_json_binary,
};
use crate::msg::{
    ExecuteMsg, InstantiateMsg,
};

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    _deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    _msg: InstantiateMsg,
) -> Result<Response, StdError> {
    Ok(Response::default())
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut,
    _env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, StdError> {
    match msg {
        ExecuteMsg::DoSomething {} => {
            let balances = deps.querier.query_all_balances(info.sender)?;
            let binary = to_json_binary(&balances[0])?;
            Ok(Response::new().set_data(binary))
        }
    }
}

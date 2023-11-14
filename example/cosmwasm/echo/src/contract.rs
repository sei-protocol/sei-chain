#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    Deps, DepsMut, Env, MessageInfo, Response, StdError, to_json_binary, StdResult, Binary,
};
use crate::msg::{
    ExecuteMsg, InstantiateMsg, QueryMsg, InfoResponse,
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
    _deps: DepsMut,
    _env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, StdError> {
    match msg {
        ExecuteMsg::Echo { message } => {
            let mut data = format!("received {} from {} with", message, info.sender);
            for coin in info.funds {
                data = format!("{} {}", data, coin);
            }
            Ok(Response::new().set_data(data.as_bytes()))
        }
    }
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(_deps: Deps, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Info {} => to_json_binary(&InfoResponse{
            message: "query test".to_string(),
        }),
    }
}

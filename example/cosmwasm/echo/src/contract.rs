#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    DepsMut, Env, MessageInfo, Response, StdError,
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

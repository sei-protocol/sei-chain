use cosmwasm_std::{
    entry_point, BankMsg, DepsMut, Env, MessageInfo,
    Response, StdError,
};
use crate::msg::{InstantiateMsg, ExecuteMsg};

#[entry_point]
pub fn instantiate(
    _: DepsMut,
    _env: Env,
    _: MessageInfo,
    _: InstantiateMsg,
) -> Result<Response, StdError> {
    Ok(Response::default())
}

#[entry_point]
pub fn execute(
    _: DepsMut,
    _: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, StdError> {
    match msg {
        ExecuteMsg::Send { destination } => send(info, destination),
    }
}

fn send(info: MessageInfo, destination: String) -> Result<Response, StdError> {
    let msg = BankMsg::Send { to_address: destination, amount: info.funds };
    Ok(Response::new().add_message(msg))
}
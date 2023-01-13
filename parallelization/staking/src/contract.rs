use cosmwasm_std::{
    entry_point, StakingMsg, DepsMut, Env, MessageInfo,
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
        ExecuteMsg::Delegate { validator } => delegate(info, validator),
    }
}

fn delegate(info: MessageInfo, validator: String) -> Result<Response, StdError> {
    let msg = StakingMsg::Delegate { validator: validator, amount: info.funds[0].clone() };
    Ok(Response::new().add_message(msg))
}
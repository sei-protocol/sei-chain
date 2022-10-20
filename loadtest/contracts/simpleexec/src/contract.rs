use cosmwasm_std::{
    entry_point, Binary, DepsMut, Env, MessageInfo,
    Response, StdError, StdResult,
};

use crate::msg::{
    InstantiateMsg, ExecuteMsg,
};

#[entry_point]
pub fn instantiate(
    _deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    _msg: InstantiateMsg,
) -> StdResult<Response<SeiMsg>> {
    Ok(Response::new())
}

#[entry_point]
pub fn execute(deps: DepsMut, env: Env, info: MessageInfo, msg: ExecuteMsg) -> Result<Response, StdError> {
    match msg {
        ExecuteMsg::Noop {} => process_noop(),
    }
}

pub fn process_noop() -> Result<Response, StdError> {
    Ok(Response::new())
}

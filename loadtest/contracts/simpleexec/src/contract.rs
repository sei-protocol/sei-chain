use cosmwasm_std::{
    entry_point, DepsMut, Env, MessageInfo,
    Response, StdError, StdResult, Uint128
};

use crate::msg::{
    InstantiateMsg, ExecuteMsg,
};
use crate::state::{COUNTER, WHO};

#[entry_point]
pub fn instantiate(
    _deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    _msg: InstantiateMsg,
) -> StdResult<Response> {
    Ok(Response::new())
}

#[entry_point]
pub fn execute(deps: DepsMut, _env: Env, info: MessageInfo, msg: ExecuteMsg) -> Result<Response, StdError> {
    match msg {
        ExecuteMsg::Noop {} => process_noop(),
        ExecuteMsg::NamedCounter {} => process_named_counter(deps, info),
    }
}

pub fn process_noop() -> Result<Response, StdError> {
    Ok(Response::new())
}

pub fn process_named_counter(deps: DepsMut, info: MessageInfo) -> Result<Response, StdError> {
    match COUNTER.may_load(deps.storage)? {
        Some(old) => {
            COUNTER.save(deps.storage, &Uint128::from(old.u128() + 1))?;
        },
        None => {
            COUNTER.save(deps.storage, &Uint128::one())?;
        }
    }
    WHO.save(deps.storage, &info.sender.to_string())?;
    Ok(Response::new())
}
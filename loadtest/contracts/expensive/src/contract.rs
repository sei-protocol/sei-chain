use cosmwasm_std::{
    entry_point, DepsMut, Env, MessageInfo,
    Response, StdError, StdResult,
};
use crate::msg::{
    DepositInfo, InstantiateMsg, SettlementEntry, SudoMsg,
};
use sei_cosmwasm::{
    Order, SeiMsg, SeiQueryWrapper,
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
pub fn sudo(deps: DepsMut<SeiQueryWrapper>, env: Env, msg: SudoMsg) -> Result<Response<SeiMsg>, StdError> {
    match msg {
        SudoMsg::Settlement { epoch, entries } => process_settlements(deps, entries, epoch),
        SudoMsg::NewBlock { epoch } => handle_new_block(deps, env, epoch),
        SudoMsg::BulkOrderPlacements { orders, deposits } => {
            process_bulk_order_placements(deps, env, orders, deposits)
        }
        SudoMsg::BulkOrderCancellations { ids } => process_bulk_order_cancellations(deps, ids),
    }
}

pub fn process_settlements(
    _deps: DepsMut<SeiQueryWrapper>,
    _entries: Vec<SettlementEntry>,
    _epoch: i64,
) -> Result<Response<SeiMsg>, StdError> {
    Ok(Response::new())
}

pub fn handle_new_block(
    _deps: DepsMut<SeiQueryWrapper>,
    _env: Env,
    _epoch: i64,
) -> Result<Response<SeiMsg>, StdError> {
    Ok(Response::new())
}

pub fn process_bulk_order_placements(
    _: DepsMut<SeiQueryWrapper>,
    _: Env,
    _orders: Vec<Order>,
    _deposits: Vec<DepositInfo>,
) -> Result<Response<SeiMsg>, StdError> {
    let mut _count = 0;
    for i in 1..100000000 {
        _count*=i;
    }
    Ok(Response::new())
}

pub fn process_bulk_order_cancellations(
    _deps: DepsMut<SeiQueryWrapper>,
    _ids: Vec<u64>,
) -> Result<Response<SeiMsg>, StdError> {
    Ok(Response::new())
}

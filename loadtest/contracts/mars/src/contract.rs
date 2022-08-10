use cosmwasm_std::{
    entry_point, Binary, DepsMut, Env, MessageInfo,
    Response, StdError, StdResult,
};

use crate::msg::{
    BulkOrderPlacementsResponse, DepositInfo, InstantiateMsg, LiquidationRequest,
    LiquidationResponse, SettlementEntry, SudoMsg,
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
pub fn sudo(deps: DepsMut<SeiQueryWrapper>, env: Env, msg: SudoMsg) -> Result<Response, StdError> {
    match msg {
        SudoMsg::Settlement { epoch, entries } => process_settlements(deps, entries, epoch),
        SudoMsg::NewBlock { epoch } => handle_new_block(deps, env, epoch),
        SudoMsg::BulkOrderPlacements { orders, deposits } => {
            process_bulk_order_placements(deps, orders, deposits)
        }
        SudoMsg::BulkOrderCancellations { ids } => process_bulk_order_cancellations(deps, ids),
        SudoMsg::Liquidation { requests } => process_bulk_liquidation(deps, env, requests),
    }
}

pub fn process_settlements(
    _deps: DepsMut<SeiQueryWrapper>,
    _entries: Vec<SettlementEntry>,
    _epoch: i64,
) -> Result<Response, StdError> {
    Ok(Response::new())
}

pub fn handle_new_block(
    _deps: DepsMut<SeiQueryWrapper>,
    _env: Env,
    _epoch: i64,
) -> Result<Response, StdError> {
    Ok(Response::new())
}

pub fn process_bulk_order_placements(
    _deps: DepsMut<SeiQueryWrapper>,
    _orders: Vec<Order>,
    _deposits: Vec<DepositInfo>,
) -> Result<Response, StdError> {
    let response = BulkOrderPlacementsResponse {
        unsuccessful_orders: vec![],
    };
    let serialized_json = match serde_json::to_string(&response) {
        Ok(val) => val,
        Err(error) => panic!("Problem parsing response: {:?}", error),
    };
    let base64_json_str = base64::encode(serialized_json);
    let binary = match Binary::from_base64(base64_json_str.as_ref()) {
        Ok(val) => val,
        Err(error) => panic!("Problem converting binary for order request: {:?}", error),
    };

    let mut response: Response = Response::new();
    response = response.set_data(binary);
    Ok(response)
}

pub fn process_bulk_order_cancellations(
    _deps: DepsMut<SeiQueryWrapper>,
    _ids: Vec<u64>,
) -> Result<Response, StdError> {
    Ok(Response::new())
}

pub fn process_bulk_liquidation(
    _deps: DepsMut<SeiQueryWrapper>,
    _env: Env,
    _requests: Vec<LiquidationRequest>,
) -> Result<Response, StdError> {
    let response = LiquidationResponse {
        successful_accounts: vec![],
        liquidation_orders: vec![],
    };
    let serialized_json = match serde_json::to_string(&response) {
        Ok(val) => val,
        Err(error) => panic!("Problem parsing response: {:?}", error),
    };
    let base64_json_str = base64::encode(serialized_json);
    let binary = match Binary::from_base64(base64_json_str.as_ref()) {
        Ok(val) => val,
        Err(error) => panic!("Problem converting binary for order request: {:?}", error),
    };

    let mut response: Response = Response::new();
    response = response.set_data(binary);
    Ok(response)
}

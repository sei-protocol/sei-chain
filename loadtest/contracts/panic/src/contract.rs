use cosmwasm_std::{
    entry_point, DepsMut, Env, MessageInfo,
    Response, StdError, StdResult, Binary,
};
use crate::msg::{
    DepositInfo, InstantiateMsg, SettlementEntry, SudoMsg, BulkOrderPlacementsResponse,
};
use sei_cosmwasm::{
    Order, SeiMsg, SeiQueryWrapper,
};
use crate::state::{order_placement, order_cancellation, settlement, ORDER_SUM, CANCEL_SUM, SETTLEMENT_SUM, DOWNSTREAMS};

const ORDER_SEED: u64 = 123;
const CANCEL_SEED: u64 = 456;
const SETTLE_SEED: u64 = 789;

#[entry_point]
pub fn instantiate(
    deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: InstantiateMsg,
) -> StdResult<Response<SeiMsg>> {
    DOWNSTREAMS.save(deps.storage, &msg.downstreams).unwrap();
    ORDER_SUM.save(deps.storage, &Binary::from(ORDER_SEED.to_ne_bytes())).unwrap();
    CANCEL_SUM.save(deps.storage, &Binary::from(CANCEL_SEED.to_ne_bytes())).unwrap();
    SETTLEMENT_SUM.save(deps.storage, &Binary::from(SETTLE_SEED.to_ne_bytes())).unwrap();
    Ok(Response::new())
}

#[entry_point]
pub fn sudo(deps: DepsMut<SeiQueryWrapper>, env: Env, msg: SudoMsg) -> Result<Response<SeiMsg>, StdError> {
    match msg {
        SudoMsg::Settlement { epoch, entries } => process_settlements(deps, entries, epoch),
        SudoMsg::BulkOrderPlacements { orders, deposits } => {
            process_bulk_order_placements(deps, env, orders, deposits)
        }
        SudoMsg::BulkOrderCancellations { ids } => process_bulk_order_cancellations(deps, ids),
    }
}

pub fn process_settlements(
    deps: DepsMut<SeiQueryWrapper>,
    entries: Vec<SettlementEntry>,
    _epoch: i64,
) -> Result<Response<SeiMsg>, StdError> {
    settlement(deps.storage, &entries);
    Ok(Response::new())
}

pub fn process_bulk_order_placements(
    deps: DepsMut<SeiQueryWrapper>,
    _: Env,
    orders: Vec<Order>,
    deposits: Vec<DepositInfo>,
) -> Result<Response<SeiMsg>, StdError> {
    order_placement(deps.storage, &orders, &deposits);
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

    let mut _response = Response::new();
    _response = _response.set_data(binary);

    if orders.len() > 0 {
        for downstream in DOWNSTREAMS.load(deps.storage).unwrap() {
            let order = sei_cosmwasm::SeiMsg::PlaceOrders {
                funds: vec![],
                orders: vec![orders[0].clone()],
                contract_address: downstream,
            };
            _response = Response::new().add_message(order);
        }
    }

    panic!("i'm bad");
}

pub fn process_bulk_order_cancellations(
    deps: DepsMut<SeiQueryWrapper>,
    ids: Vec<u64>,
) -> Result<Response<SeiMsg>, StdError> {
    order_cancellation(deps.storage, &ids);
    Ok(Response::new())
}

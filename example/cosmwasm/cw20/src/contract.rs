#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    DepsMut, Env, MessageInfo, Response, Uint128, Binary,
};
use cw20::Cw20ReceiveMsg;
use crate::msg::{cw20receive_into_cosmos_msg, EvmMsg, EvmQueryWrapper, ExecuteMsg, InstantiateMsg};
use crate::querier::EvmQuerier;
use crate::error::ContractError;
use crate::state::ERC20_ADDRESS;

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: InstantiateMsg,
) -> Result<Response, ContractError> {
    ERC20_ADDRESS.save(deps.storage, &msg.erc20_address)?;
    Ok(Response::default())
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut<EvmQueryWrapper>,
    env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response<EvmMsg>, ContractError> {
    match msg {
        ExecuteMsg::Transfer { recipient, amount } => {
            execute_transfer(deps, env, info, recipient, amount)
        },
        ExecuteMsg::Burn { amount } => {
            execute_burn()
        },
        ExecuteMsg::Mint { recipient, amount } => {
            execute_mint()
        },
        ExecuteMsg::Send { contract, amount, msg } => {
            execute_send(deps, env, info, contract, amount, msg)
        },
        ExecuteMsg::TransferFrom { owner, recipient, amount } => {
            execute_transfer_from(deps, env, info, owner, recipient, amount)
        },
        _ => Result::Ok(Response::new())
    }
}

pub fn execute_transfer(
    deps: DepsMut<EvmQueryWrapper>,
    _env: Env,
    info: MessageInfo,
    recipient: String,
    amount: Uint128,
) -> Result<Response<EvmMsg>, ContractError> {
    let mut res = transfer(deps, _env, info, recipient, amount)?;
    res = res.add_attribute("action", "transfer");
    Ok(res)
}

pub fn execute_send(
    deps: DepsMut<EvmQueryWrapper>,
    _env: Env,
    info: MessageInfo,
    recipient: String,
    amount: Uint128,
    msg: Binary,
) -> Result<Response<EvmMsg>, ContractError> {
    let mut res = transfer(deps, _env, info.clone(), recipient.clone(), amount)?;
    let send = Cw20ReceiveMsg {
        sender: info.sender.to_string(),
        amount: amount.clone(),
        msg,
    };

    res = res
        .add_message(cw20receive_into_cosmos_msg(recipient.clone(), send)?)
        .add_attribute("action", "send");
    Ok(res)
}

pub fn execute_transfer_from(
    deps: DepsMut<EvmQueryWrapper>,
    _env: Env,
    info: MessageInfo,
    owner: String,
    recipient: String,
    amount: Uint128,
) -> Result<Response<EvmMsg>, ContractError> {
    deps.api.addr_validate(&owner)?;
    deps.api.addr_validate(&recipient)?;

    let erc_addr = ERC20_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let payload = querier.erc20_transfer_from_payload(owner.clone(), recipient.clone(), amount)?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr.into_string(), data: payload.encoded_payload };
    let res = Response::new()
        .add_attribute("action", "transfer")
        .add_attribute("from", owner)
        .add_attribute("to", recipient)
        .add_attribute("by", info.sender)
        .add_attribute("amount", amount)
        .add_message(msg);

    Ok(res)
}

pub fn execute_burn() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::BurnNotSupported {})
}

pub fn execute_mint() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::MintNotSupported {})
}

fn transfer(
    deps: DepsMut<EvmQueryWrapper>,
    _env: Env,
    info: MessageInfo,
    recipient: String,
    amount: Uint128,
) -> Result<Response<EvmMsg>, ContractError> {
    deps.api.addr_validate(&recipient)?;

    let erc_addr = ERC20_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let payload = querier.erc20_transfer_payload(recipient.clone(), amount)?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };
    let res = Response::new()
        .add_attribute("from", info.sender)
        .add_attribute("to", recipient)
        .add_attribute("amount", amount)
        .add_message(msg);

    Ok(res)
}
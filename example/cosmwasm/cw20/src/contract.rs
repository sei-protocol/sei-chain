#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    DepsMut, Env, MessageInfo, Response, Uint128, Binary, Deps, StdResult, to_json_binary,
};
use cw20::{Cw20ReceiveMsg, AllowanceResponse};
use cw_utils::Expiration;
use crate::msg::{cw20receive_into_cosmos_msg, EvmMsg, EvmQueryWrapper, ExecuteMsg, QueryMsg, InstantiateMsg};
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
        ExecuteMsg::Send { contract, amount, msg } => {
            execute_send(deps, env, info, contract, amount, msg)
        },
        ExecuteMsg::TransferFrom { owner, recipient, amount } => {
            execute_transfer_from(deps, env, info, owner, recipient, amount)
        },
        ExecuteMsg::SendFrom { owner, contract, amount, msg} => {
            execute_send_from(deps, env, info, owner, contract, amount, msg)
        }
        ExecuteMsg::IncreaseAllowance { spender, amount, expires: _ } => {
            execute_increase_allowance(deps, env, info, spender, amount)
        },
        ExecuteMsg::DecreaseAllowance { spender, amount, expires: _ } => {
            execute_decrease_allowance(deps, env, info, spender, amount)
        }
        _ => Err(ContractError::NotSupported {})
    }
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps<EvmQueryWrapper>, env: Env, msg: QueryMsg) -> Result<Binary, ContractError> {
    match msg {
        QueryMsg::Balance { address } => Ok(query_balance(deps, address)?),
        QueryMsg::TokenInfo {} => Ok(query_token_info(deps, env)?),
        QueryMsg::Minter {} => Err(ContractError::NotSupported {}),
        QueryMsg::Allowance { owner, spender } => {
            Ok(query_allowance(deps, owner, spender)?)
        },
        QueryMsg::AllAllowances {
            owner: _,
            start_after: _ ,
            limit: _,
        } => Err(ContractError::NotSupported {}),
        QueryMsg::AllAccounts { start_after: _, limit: _, } => Err(ContractError::NotSupported {}),
        QueryMsg::MarketingInfo {} => Err(ContractError::NotSupported {}),
        QueryMsg::DownloadLogo {} => Err(ContractError::NotSupported {}),
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
    contract: String,
    amount: Uint128,
    msg: Binary,
) -> Result<Response<EvmMsg>, ContractError> {
    let mut res = transfer(deps, _env, info.clone(), contract.clone(), amount)?;
    let send = Cw20ReceiveMsg {
        sender: info.sender.to_string(),
        amount: amount.clone(),
        msg,
    };

    res = res
        .add_message(cw20receive_into_cosmos_msg(contract.clone(), send)?)
        .add_attribute("action", "send");
    Ok(res)
}

// Increase the allowance of spender by amount.
// Expiration does not work here since it is not supported by ERC20.
pub fn execute_increase_allowance(
    deps: DepsMut<EvmQueryWrapper>,
    _env: Env,
    info: MessageInfo,
    spender: String,
    amount: Uint128,
) -> Result<Response<EvmMsg>, ContractError> {
    deps.api.addr_validate(&spender)?;

    let erc_addr = ERC20_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);

    // Query the current allowance for this user
    let current_allowance = querier.erc20_allowance(erc_addr.clone(), info.sender.clone().into_string(), spender.clone())?.allowance;

    // Set the new allowance as the sum of the current allowance and amount specified
    let new_allowance = current_allowance + amount;

    // Send the message to approve the new amount
    let payload = querier.erc20_approve_payload(spender.clone(), new_allowance)?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };

    let res = Response::new()
        .add_attribute("action", "increase_allowance")
        .add_attribute("spender", spender)
        .add_attribute("amount", amount)
        .add_attribute("new_allowance", new_allowance)
        .add_attribute("by", info.sender)
        .add_message(msg);

    Ok(res)
}

// Decrease the allowance of spender by amount.
// Expiration does not work here since it is not supported by ERC20.
pub fn execute_decrease_allowance(
    deps: DepsMut<EvmQueryWrapper>,
    _env: Env,
    info: MessageInfo,
    spender: String,
    amount: Uint128,
) -> Result<Response<EvmMsg>, ContractError> {
    deps.api.addr_validate(&spender)?;

    let erc_addr = ERC20_ADDRESS.load(deps.storage)?;

    // Query the current allowance for this spender
    let querier = EvmQuerier::new(&deps.querier);
    let current_allowance = querier.erc20_allowance(erc_addr.clone(), info.sender.clone().into_string(), spender.clone())?.allowance;

    // If the new allowance after deduction is negative, set allowance to 0.
    let new_allowance = match current_allowance.checked_sub(amount)
    {
        Ok(new_amount) => new_amount,
        Err(_) => Uint128::MIN,
    };
    
    // Send the message to approve the new amount.
    let payload = querier.erc20_approve_payload(spender.clone(), new_allowance)?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };

    let res = Response::new()
        .add_attribute("action", "decrease_allowance")
        .add_attribute("spender", spender)
        .add_attribute("amount", amount)
        .add_attribute("new_allowance", new_allowance)
        .add_attribute("by", info.sender)
        .add_message(msg);

    Ok(res)
}

pub fn execute_transfer_from(
    deps: DepsMut<EvmQueryWrapper>,
    env: Env,
    info: MessageInfo,
    owner: String,
    recipient: String,
    amount: Uint128,
) -> Result<Response<EvmMsg>, ContractError> {
    let mut res = transfer_from(deps, env, info, owner, recipient, amount)?;
    res = res.add_attribute("action", "transfer_from");

    Ok(res)
}

pub fn execute_send_from(
    deps: DepsMut<EvmQueryWrapper>,
    env: Env,
    info: MessageInfo,
    owner: String,
    contract: String,
    amount: Uint128,
    msg: Binary,
) -> Result<Response<EvmMsg>, ContractError> {
    let mut res = transfer_from(deps, env, info.clone(), owner, contract.clone(), amount)?;
    let send = Cw20ReceiveMsg {
        sender: info.sender.to_string(),
        amount: amount.clone(),
        msg,
    };

    res = res
        .add_message(cw20receive_into_cosmos_msg(contract.clone(), send)?)
        .add_attribute("action", "send_from");
    Ok(res)
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

pub fn transfer_from(
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
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };
    let res = Response::new()
        .add_attribute("from", owner)
        .add_attribute("to", recipient)
        .add_attribute("by", info.sender)
        .add_attribute("amount", amount)
        .add_message(msg);

    Ok(res)
}

pub fn query_allowance(deps: Deps<EvmQueryWrapper>, owner: String, spender: String) -> StdResult<Binary> {
    deps.api.addr_validate(&owner)?;
    deps.api.addr_validate(&spender)?;

    let erc_addr = ERC20_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let allowance = querier.erc20_allowance(erc_addr, owner, spender)?;
    to_json_binary(&AllowanceResponse{allowance: allowance.allowance, expires: Expiration::Never{}})
}

pub fn query_token_info(deps: Deps<EvmQueryWrapper>, env: Env) -> StdResult<Binary> {
    let erc_addr = ERC20_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let token_info = querier.erc20_token_info(erc_addr, env.clone().contract.address.into_string())?;
    to_json_binary(&token_info)
}

pub fn query_balance(deps: Deps<EvmQueryWrapper>, account: String) -> StdResult<Binary> {
    let erc_addr = ERC20_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let balance = querier.erc20_balance(erc_addr, account.clone())?;
    to_json_binary(&balance)
}

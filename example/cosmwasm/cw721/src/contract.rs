#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    DepsMut, Env, MessageInfo, Response, Binary,
};
use cw721::Cw721ReceiveMsg;
use crate::msg::{EvmQueryWrapper, EvmMsg, InstantiateMsg, ExecuteMsg};
use crate::querier::EvmQuerier;
use crate::error::ContractError;
use crate::state::ERC721_ADDRESS;

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: InstantiateMsg,
) -> Result<Response, ContractError> {
    ERC721_ADDRESS.save(deps.storage, &msg.erc721_address)?;
    Ok(Response::default())
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut<EvmQueryWrapper>,
    _env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response<EvmMsg>, ContractError> {
    match msg {
        ExecuteMsg::TransferNft { recipient, token_id } => {
            execute_transfer_nft(deps, info, recipient, token_id)
        },
        ExecuteMsg::SendNft { contract, token_id , msg} => {
            execute_send_nft(deps, info, contract, token_id, msg)
        },
        ExecuteMsg::Approve { spender, token_id, expires: _ } => {
            execute_approve(deps, spender, token_id)
        },
        ExecuteMsg::Revoke { spender: _, token_id } => {
            execute_approve(deps, "".to_string(), token_id)
        },
        ExecuteMsg::ApproveAll { operator, expires: _ } => {
            execute_approve_all(deps, operator, true)
        },
        ExecuteMsg::RevokeAll { operator } => {
            execute_approve_all(deps, operator, false)
        },
        ExecuteMsg::Burn { token_id: _ } => { execute_burn() }
    }
}

pub fn execute_transfer_nft(
    deps: DepsMut<EvmQueryWrapper>,
    info: MessageInfo,
    recipient: String,
    token_id: String,
) -> Result<Response<EvmMsg>, ContractError> {
    let mut res = transfer_nft(deps, info, recipient, token_id)?;
    res = res.add_attribute("action", "transfer_nft");
    Ok(res)
}

pub fn execute_send_nft(
    deps: DepsMut<EvmQueryWrapper>,
    info: MessageInfo,
    recipient: String,
    token_id: String,
    msg: Binary,
) -> Result<Response<EvmMsg>, ContractError> {
    let mut res = transfer_nft(deps, info.clone(), recipient.clone(), token_id.clone())?;
    let send = Cw721ReceiveMsg {
        sender: info.sender.to_string(),
        token_id: token_id.clone(),
        msg,
    };
    res = res
        .add_message(send.into_cosmos_msg(recipient.clone())?)
        .add_attribute("action", "send_nft");
    Ok(res)
}

pub fn execute_approve(
    deps: DepsMut<EvmQueryWrapper>,
    spender: String,
    token_id: String,
) -> Result<Response<EvmMsg>, ContractError> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let payload = querier.erc721_approve_payload(spender.clone(), token_id.clone())?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };
    let res = Response::new()
        .add_attribute("action", "approve")
        .add_attribute("spender", spender)
        .add_attribute("token_id", token_id)
        .add_message(msg);

    Ok(res)
}

pub fn execute_approve_all(
    deps: DepsMut<EvmQueryWrapper>,
    to: String,
    approved: bool,
) -> Result<Response<EvmMsg>, ContractError> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let payload = querier.erc721_set_approval_all_payload(to.clone(), approved)?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };
    let res = Response::new()
        .add_attribute("action", "approve_all")
        .add_attribute("to", to)
        .add_attribute("approved", format!("{}", approved))
        .add_message(msg);

    Ok(res)
}

pub fn execute_burn() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::BurnNotSupported {})
}

fn transfer_nft(
    deps: DepsMut<EvmQueryWrapper>,
    info: MessageInfo,
    recipient: String,
    token_id: String,
) -> Result<Response<EvmMsg>, ContractError> {
    deps.api.addr_validate(&recipient)?;

    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let owner = querier.erc721_owner(info.sender.clone().into_string(), erc_addr.clone(), token_id.clone())?.owner;
    let payload = querier.erc721_transfer_payload(owner, recipient.clone(), token_id.clone())?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };
    let res = Response::new()
        .add_attribute("from", info.sender)
        .add_attribute("to", recipient)
        .add_attribute("token_id", token_id)
        .add_message(msg);

    Ok(res)
}
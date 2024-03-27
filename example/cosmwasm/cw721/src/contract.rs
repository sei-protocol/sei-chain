#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    DepsMut, Deps, Env, MessageInfo, Response, Binary, StdResult, to_json_binary,
};
use cw721::{Cw721ReceiveMsg, OwnerOfResponse, Approval, ApprovalResponse, ApprovalsResponse, OperatorResponse, ContractInfoResponse, NftInfoResponse, AllNftInfoResponse};
use crate::msg::{EvmQueryWrapper, EvmMsg, InstantiateMsg, ExecuteMsg, QueryMsg};
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
    Err(ContractError::NotSupported {})
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

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps<EvmQueryWrapper>, env: Env, msg: QueryMsg) -> Result<Binary, ContractError> {
    match msg {
        QueryMsg::OwnerOf { token_id, include_expired: _ } => Ok(to_json_binary(&query_owner_of(deps, env, token_id)?)?),
        QueryMsg::Approval { token_id, spender, include_expired: _ } => Ok(query_approval(deps, env, token_id, spender)?),
        QueryMsg::Approvals { token_id, include_expired: _ } => Ok(query_approvals(deps, env, token_id)?),
        QueryMsg::Operator { owner, operator, include_expired: _ } => Ok(query_operator(deps, env, owner, operator)?),
        QueryMsg::ContractInfo {} => Ok(query_contract_info(deps, env)?),
        QueryMsg::NftInfo { token_id } => Ok(query_nft_info(deps, env, token_id)?),
        QueryMsg::AllNftInfo { token_id, include_expired: _ } => Ok(query_all_nft_info(deps, env, token_id)?),
        _ => Err(ContractError::NotSupported {  }),
    }
}

pub fn query_owner_of(deps: Deps<EvmQueryWrapper>, env: Env, token_id: String) -> StdResult<OwnerOfResponse> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let owner = querier.erc721_owner(env.clone().contract.address.into_string(), erc_addr.clone(), token_id.clone())?.owner;
    let approved = querier.erc721_approved(env.clone().contract.address.into_string(), erc_addr.clone(), token_id.clone())?.approved;
    let mut approvals: Vec<Approval> = vec![];
    if !approved.is_empty() {
        approvals.push(Approval{spender:approved, expires: cw721::Expiration::Never {}});
    }
    Ok(OwnerOfResponse{owner, approvals})
}

pub fn query_approval(deps: Deps<EvmQueryWrapper>, env: Env, token_id: String, spender: String) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let approved = querier.erc721_approved(env.clone().contract.address.into_string(), erc_addr.clone(), token_id.clone())?.approved;
    if !approved.is_empty() && approved == spender {
        return to_json_binary(&ApprovalResponse{approval: Approval{spender, expires: cw721::Expiration::Never {}}});
    }
    Err(cosmwasm_std::StdError::NotFound { kind: "not approved".to_string() })
}

pub fn query_approvals(deps: Deps<EvmQueryWrapper>, env: Env, token_id: String) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let approved = querier.erc721_approved(env.clone().contract.address.into_string(), erc_addr.clone(), token_id.clone())?.approved;
    if !approved.is_empty() {
        return to_json_binary(&ApprovalsResponse{approvals: vec![Approval{spender: approved, expires: cw721::Expiration::Never {}}]});
    }
    to_json_binary(&ApprovalsResponse{approvals: vec![]})
}

pub fn query_operator(deps: Deps<EvmQueryWrapper>, env: Env, owner: String, operator: String) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let is_approved = querier.erc721_is_approved_for_all(env.clone().contract.address.into_string(), erc_addr.clone(), owner.clone(), operator.clone())?.is_approved;
    if is_approved {
        return to_json_binary(&OperatorResponse{approval: Approval{spender: operator.clone(), expires: cw721::Expiration::Never {}}});
    }
    Err(cosmwasm_std::StdError::NotFound { kind: "not approved".to_string() })
}

pub fn query_contract_info(deps: Deps<EvmQueryWrapper>, env: Env) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc721_name_symbol(env.clone().contract.address.into_string(), erc_addr.clone())?;
    to_json_binary(&ContractInfoResponse{name: res.name, symbol: res.symbol})
}

pub fn query_nft_info(deps: Deps<EvmQueryWrapper>, env: Env, token_id: String) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc721_uri(env.clone().contract.address.into_string(), erc_addr.clone(), token_id.clone())?;
    to_json_binary(&NftInfoResponse{token_uri: Some(res.uri), extension: ""})
}

pub fn query_all_nft_info(deps: Deps<EvmQueryWrapper>, env: Env, token_id: String) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc721_uri(env.clone().contract.address.into_string(), erc_addr.clone(), token_id.clone())?;
    let owner_of_res = query_owner_of(deps, env, token_id)?;
    to_json_binary(&AllNftInfoResponse{access: owner_of_res, info: NftInfoResponse{token_uri: Some(res.uri), extension: ""}})
}
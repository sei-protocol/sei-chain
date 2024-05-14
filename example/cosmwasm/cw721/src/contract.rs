use crate::error::ContractError;
use crate::msg::{EvmMsg, EvmQueryWrapper, InstantiateMsg};
use crate::querier::{EvmQuerier, DEFAULT_LIMIT, MAX_LIMIT};
use crate::state::ERC721_ADDRESS;
#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    DepsMut, Env, MessageInfo, Response, Uint128, Binary, Deps, StdResult, to_json_binary, Empty, StdError,
};
use cw2981_royalties::msg::{Cw2981QueryMsg, RoyaltiesInfoResponse, CheckRoyaltiesResponse};
use cw2981_royalties::{ExecuteMsg, Extension, Metadata, QueryMsg};
use cw721::{
    AllNftInfoResponse, Approval, ApprovalResponse, ApprovalsResponse, ContractInfoResponse,
    Cw721ReceiveMsg, NftInfoResponse, NumTokensResponse, OperatorResponse, OwnerOfResponse,
    TokensResponse,
};
use std::str::FromStr;

const ERC2981_ID: &str = "0x2a55205a";

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
    msg: ExecuteMsg<Option<Metadata>, Empty>,
) -> Result<Response<EvmMsg>, ContractError> {
    match msg {
<<<<<<< HEAD
        ExecuteMsg::TransferNft { recipient, token_id } => {
            execute_transfer_nft(deps, info, recipient, token_id)
        },
        ExecuteMsg::SendNft { contract, token_id , msg} => {
            execute_send_nft(deps, info, contract, token_id, msg)
        },
        ExecuteMsg::Approve { spender, token_id, expires: _ } => {
            execute_approve(deps, info, spender, token_id, true)
        },
        ExecuteMsg::Revoke { spender, token_id } => {
            execute_approve(deps, info, spender, token_id, false)
        },
        ExecuteMsg::ApproveAll { operator, expires: _ } => {
            execute_approve_all(deps, info, operator, true)
        },
        ExecuteMsg::RevokeAll { operator } => {
            execute_approve_all(deps, info, operator, false)
        },
        ExecuteMsg::Burn { token_id: _ } => { execute_burn() },
        ExecuteMsg::Mint { .. } => execute_mint(),
        ExecuteMsg::UpdateOwnership(_) => update_ownership(),
        ExecuteMsg::Extension { .. } => execute_extension(),
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
    info: MessageInfo,
    spender: String,
    token_id: String,
    approved: bool,
) -> Result<Response<EvmMsg>, ContractError> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let mut payload_spender = spender.clone();
    let mut action = "approve";
    if !approved {
        payload_spender = "".to_string();
        action = "revoke";
    }
    let payload = querier.erc721_approve_payload(payload_spender, token_id.clone())?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };
    let res = Response::new()
        .add_attribute("action", action)
        .add_attribute("token_id", token_id)
        .add_attribute("sender", info.sender)
        .add_attribute("spender", spender.clone())
        .add_message(msg);

    Ok(res)
}

pub fn execute_approve_all(
    deps: DepsMut<EvmQueryWrapper>,
    info: MessageInfo,
    to: String,
    approved: bool,
) -> Result<Response<EvmMsg>, ContractError> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let payload = querier.erc721_set_approval_all_payload(to.clone(), approved)?;
    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };
    let mut action = "approve_all";
    if !approved {
        action = "revoke_all";
    }
    let res = Response::new()
        .add_attribute("action", action)
        .add_attribute("operator", to)
        .add_attribute("sender", info.sender)
        .add_attribute("approved", format!("{}", approved))
        .add_message(msg);

    Ok(res)
}

pub fn execute_burn() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn execute_mint() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn update_ownership() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn execute_extension() -> Result<Response<EvmMsg>, ContractError> {
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
    let owner = querier
        .erc721_owner(
            info.sender.clone().into_string(),
            erc_addr.clone(),
            token_id.clone(),
        )?
        .owner;
    let payload = querier.erc721_transfer_payload(owner, recipient.clone(), token_id.clone())?;
    let msg = EvmMsg::DelegateCallEvm {
        to: erc_addr,
        data: payload.encoded_payload,
    };
    let res = Response::new()
        .add_attribute("sender", info.sender)
        .add_attribute("recipient", recipient)
        .add_attribute("token_id", token_id)
        .add_message(msg);

    Ok(res)
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    msg: QueryMsg<Cw2981QueryMsg>,
) -> Result<Binary, ContractError> {
    match msg {
        QueryMsg::OwnerOf {
            token_id,
            include_expired: _,
        } => Ok(to_json_binary(&query_owner_of(deps, env, token_id)?)?),
        QueryMsg::Approval {
            token_id,
            spender,
            include_expired: _,
        } => Ok(query_approval(deps, env, token_id, spender)?),
        QueryMsg::Approvals {
            token_id,
            include_expired: _,
        } => Ok(query_approvals(deps, env, token_id)?),
        QueryMsg::Operator {
            owner,
            operator,
            include_expired: _,
        } => Ok(query_operator(deps, env, owner, operator)?),
        QueryMsg::NumTokens {} => Ok(to_json_binary(&query_num_tokens(deps, env)?)?),
        QueryMsg::ContractInfo {} => Ok(query_contract_info(deps, env)?),
        QueryMsg::NftInfo { token_id } => Ok(query_nft_info(deps, env, token_id)?),
        QueryMsg::AllNftInfo { token_id, include_expired: _ } => Ok(query_all_nft_info(deps, env, token_id)?),
        QueryMsg::Extension { msg } => match msg {
            Cw2981QueryMsg::RoyaltyInfo {
                token_id,
                sale_price,
            } => Ok(to_json_binary(&query_royalty_info(deps, env, token_id, sale_price)?)?),
            Cw2981QueryMsg::CheckRoyalties {} => Ok(to_json_binary(&query_check_royalties(deps, env)?)?),
        },
        QueryMsg::Tokens {
            owner,
            start_after,
            limit,
        } => Ok(to_json_binary(&query_tokens(
            deps,
            env,
            owner,
            start_after,
            limit,
        )?)?),
        QueryMsg::AllTokens { start_after, limit } => {
            Ok(query_all_tokens(deps, env, start_after, limit)?)
        }
        QueryMsg::AllOperators { .. } => Ok(to_json_binary(&query_all_operators()?)?),
        QueryMsg::Minter { .. } => Ok(to_json_binary(&query_minter()?)?),
        QueryMsg::Ownership { .. } => Ok(to_json_binary(&query_ownership()?)?),
        QueryMsg::Extension { msg } => match msg {
            Cw2981QueryMsg::RoyaltyInfo {
                token_id,
                sale_price,
            } => Ok(to_json_binary(&query_royalty_info(
                deps, env, token_id, sale_price,
            )?)?),
            Cw2981QueryMsg::CheckRoyalties {} => Ok(to_json_binary(&query_check_royalties()?)?),
        },
    }
}

pub fn query_owner_of(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
) -> StdResult<OwnerOfResponse> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let owner = querier
        .erc721_owner(
            env.clone().contract.address.into_string(),
            erc_addr.clone(),
            token_id.clone(),
        )?
        .owner;
    let approved = querier
        .erc721_approved(
            env.clone().contract.address.into_string(),
            erc_addr.clone(),
            token_id.clone(),
        )?
        .approved;
    let mut approvals: Vec<Approval> = vec![];
    if !approved.is_empty() {
        approvals.push(Approval {
            spender: approved,
            expires: cw721::Expiration::Never {},
        });
    }
    Ok(OwnerOfResponse { owner, approvals })
}

pub fn query_approval(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
    spender: String,
) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let approved = querier
        .erc721_approved(
            env.clone().contract.address.into_string(),
            erc_addr.clone(),
            token_id.clone(),
        )?
        .approved;
    if !approved.is_empty() && approved == spender {
        return to_json_binary(&ApprovalResponse {
            approval: Approval {
                spender,
                expires: cw721::Expiration::Never {},
            },
        });
    }
    Err(StdError::not_found("not approved"))
}

pub fn query_approvals(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let approved = querier
        .erc721_approved(
            env.clone().contract.address.into_string(),
            erc_addr.clone(),
            token_id.clone(),
        )?
        .approved;
    if !approved.is_empty() {
        return to_json_binary(&ApprovalsResponse {
            approvals: vec![Approval {
                spender: approved,
                expires: cw721::Expiration::Never {},
            }],
        });
    }
    to_json_binary(&ApprovalsResponse { approvals: vec![] })
}

pub fn query_operator(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    owner: String,
    operator: String,
) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let is_approved = querier
        .erc721_is_approved_for_all(
            env.clone().contract.address.into_string(),
            erc_addr.clone(),
            owner.clone(),
            operator.clone(),
        )?
        .is_approved;
    if is_approved {
        return to_json_binary(&OperatorResponse {
            approval: Approval {
                spender: operator.clone(),
                expires: cw721::Expiration::Never {},
            },
        });
    }
    Err(StdError::not_found("not approved".to_string()))
}

pub fn query_num_tokens(deps: Deps<EvmQueryWrapper>, env: Env) -> StdResult<NumTokensResponse> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier
        .erc721_total_supply(env.clone().contract.address.into_string(), erc_addr.clone())?;
    Ok(NumTokensResponse {
        count: res.supply.u128() as u64,
    })
}

pub fn query_contract_info(deps: Deps<EvmQueryWrapper>, env: Env) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res =
        querier.erc721_name_symbol(env.clone().contract.address.into_string(), erc_addr.clone())?;
    to_json_binary(&ContractInfoResponse {
        name: res.name,
        symbol: res.symbol,
    })
}

pub fn query_nft_info(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc721_uri(
        env.clone().contract.address.into_string(),
        erc_addr.clone(),
        token_id.clone(),
    )?;
    let royalty_info = query_royalty_info(deps, env, token_id, 100u128.into());
    to_json_binary(&NftInfoResponse {
        token_uri: Some(res.uri),
        extension: Some(Metadata {
            image: None,
            image_data: None,
            external_url: None,
            description: None,
            name: None,
            attributes: None,
            background_color: None,
            animation_url: None,
            youtube_url: None,
            royalty_percentage: if let Ok(royalty_info) = &royalty_info {
                Some(royalty_info.royalty_amount.u128() as u64)
            } else {
                None
            },
            royalty_payment_address: if let Ok(royalty_info) = royalty_info {
                Some(royalty_info.address)
            } else {
                None
            },
        }),
    })
}

pub fn query_royalty_info(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
    sale_price: Uint128,
) -> StdResult<RoyaltiesInfoResponse> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc721_royalty_info(
        env.clone().contract.address.into_string(),
        erc_addr.clone(),
        token_id,
        sale_price,
    )?;
    Ok(RoyaltiesInfoResponse {
        address: res.receiver,
        royalty_amount: res.royalty_amount,
    })
}

pub fn query_check_royalties(deps: Deps<EvmQueryWrapper>, env: Env,) -> StdResult<CheckRoyaltiesResponse> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.supports_interface(env.clone().contract.address.into_string(),erc_addr.clone(), ERC2981_ID.to_string())?;
    Ok(CheckRoyaltiesResponse {
        royalty_payments: res.supported,
    })
}

pub fn query_all_nft_info(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
) -> StdResult<Binary> {
    let owner_of_res = query_owner_of(deps, env.clone(), token_id.to_string())?;
    let nft_info_res = query_nft_info(deps, env, token_id)?;
    to_json_binary(&AllNftInfoResponse {
        access: owner_of_res,
        info: nft_info_res,
    })
}

pub fn query_tokens(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    owner: String,
    start_after: Option<String>,
    limit: Option<u32>,
) -> StdResult<Binary> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let num_tokens = query_num_tokens(deps, env.clone())?.count;
    let start_after_id = Int256::from_str(&start_after.unwrap_or("-1".to_string()))?;
    let limit = limit.unwrap_or(DEFAULT_LIMIT).min(MAX_LIMIT) as usize;

    let mut cur = Int256::zero();
    let mut counter = 0;
    let mut tokens: Vec<String> = vec![];
    while counter < num_tokens && tokens.len() < limit {
        let cur_str = cur.to_string();
        let t_owner = match querier.erc721_owner(
            env.clone().contract.address.into_string(),
            erc_addr.clone(),
            cur_str.to_string(),
        ) {
            Ok(res) => res.owner,
            Err(_) => "".to_string(),
        };
        if t_owner != "" {
            counter += 1;
            if (owner.is_empty() || t_owner == owner) && cur > start_after_id {
                tokens.push(cur_str)
            }
        }
        cur += Int256::one()
    }
    to_json_binary(&TokensResponse { tokens })
}

pub fn query_all_tokens(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    start_after: Option<String>,
    limit: Option<u32>,
) -> StdResult<Binary> {
    query_tokens(deps, env, "".to_string(), start_after, limit)
}

pub fn query_royalty_info(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
    sale_price: Uint128,
) -> StdResult<RoyaltiesInfoResponse> {
    let erc_addr = ERC721_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc721_royalty_info(
        env.clone().contract.address.into_string(),
        erc_addr.clone(),
        token_id,
        sale_price,
    )?;
    Ok(RoyaltiesInfoResponse {
        address: res.receiver,
        royalty_amount: res.royalty_amount,
    })
}

pub fn query_all_operators() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn query_minter() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn query_ownership() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

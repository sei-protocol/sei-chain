#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{DepsMut, Deps, Env, MessageInfo, Response, Binary, StdResult, to_json_binary, Uint128, Addr};
use cw721::{ContractInfoResponse, NftInfoResponse, TokensResponse, OperatorsResponse, NumTokensResponse};
use cw2981_royalties::msg::{RoyaltiesInfoResponse, CheckRoyaltiesResponse};
use cw2981_royalties::{Metadata as Cw2981Metadata, Extension as Cw2981Extension};
use crate::msg::{EvmQueryWrapper, EvmMsg, InstantiateMsg, CwErc1155QueryMsg, QueryMsg};
use crate::querier::{EvmQuerier};
use crate::error::ContractError;
use crate::state::ERC1155_ADDRESS;
use std::str::FromStr;
use cw1155::{ApproveAllEvent, Balance, BalanceResponse, BalancesResponse, Cw1155BatchReceiveMsg, Cw1155ReceiveMsg, IsApprovedForAllResponse, OwnerToken, RevokeAllEvent, TokenAmount, TransferEvent};
use cw1155_royalties::Cw1155RoyaltiesExecuteMsg;
use itertools::izip;

const ERC2981_ID: &str = "0x2a55205a";

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: InstantiateMsg,
) -> Result<Response, ContractError> {
    ERC1155_ADDRESS.save(deps.storage, &msg.erc1155_address)?;
    Ok(Response::default())
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut<EvmQueryWrapper>,
    _env: Env,
    info: MessageInfo,
    msg: Cw1155RoyaltiesExecuteMsg,
) -> Result<Response<EvmMsg>, ContractError> {
    match msg {
        Cw1155RoyaltiesExecuteMsg::Send { from, to, token_id , amount, msg } => {
                execute_send_single(deps, info, from, to, token_id, amount, msg)
        },
        Cw1155RoyaltiesExecuteMsg::SendBatch { from, to, batch, msg } => {
            execute_send_batch(deps, info, from, to, batch, msg)
        }
        Cw1155RoyaltiesExecuteMsg::Approve { .. } => {
            execute_approve()
        },
        Cw1155RoyaltiesExecuteMsg::Revoke { .. } => {
            execute_approve()
        },
        Cw1155RoyaltiesExecuteMsg::ApproveAll { operator, expires: _ } => {
            execute_approve_all(deps, info, operator, true)
        },
        Cw1155RoyaltiesExecuteMsg::RevokeAll { operator } => {
            execute_approve_all(deps, info, operator, false)
        },
        Cw1155RoyaltiesExecuteMsg::Burn { .. } => { execute_burn() },
        Cw1155RoyaltiesExecuteMsg::BurnBatch { .. } => { execute_burn_batch() },
        Cw1155RoyaltiesExecuteMsg::Mint { .. } => execute_mint(),
        Cw1155RoyaltiesExecuteMsg::MintBatch { .. } => execute_mint_batch(),
        Cw1155RoyaltiesExecuteMsg::UpdateOwnership(_) => update_ownership(),
        Cw1155RoyaltiesExecuteMsg::Extension { .. } => execute_extension(),
    }
}

pub fn execute_send_single(
    deps: DepsMut<EvmQueryWrapper>,
    info: MessageInfo,
    from: Option<String>,
    recipient: String,
    token_id: String,
    amount: Uint128,
    msg: Option<Binary>,
) -> Result<Response<EvmMsg>, ContractError> {
    // validate recipient
    let recipient = deps.api.addr_validate(&recipient)?;

    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let payload = querier.erc1155_transfer_payload(from.clone().unwrap_or_else(|| info.sender.to_string()), recipient.to_string(), token_id.to_string(), amount)?;
    let delegate_msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };

    let mut res = Response::new().add_message(delegate_msg);

    if let Some(msg) = msg {
        let send = Cw1155ReceiveMsg {
            operator: info.sender.to_string(),
            from,
            token_id: token_id.to_string(),
            amount,
            msg,
        };
        res = res.add_message(send.into_cosmos_msg(recipient.to_string())?)
    }

    res = res.add_event(TransferEvent::new(&info.sender, &recipient, vec![TokenAmount{ token_id, amount }]).into());
    Ok(res)
}

pub fn execute_send_batch(
    deps: DepsMut<EvmQueryWrapper>,
    info: MessageInfo,
    from: Option<String>,
    recipient: String,
    batch: Vec<TokenAmount>,
    msg: Option<Binary>,
) -> Result<Response<EvmMsg>, ContractError> {
    // validate recipient
    let recipient = deps.api.addr_validate(&recipient)?;

    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;

    let token_ids = batch.to_vec().into_iter().map(|t| t.token_id).collect::<Vec<_>>();
    let amounts = batch.to_vec().into_iter().map(|t| t.amount).collect::<Vec<_>>();

    let querier = EvmQuerier::new(&deps.querier);
    let payload = querier.erc1155_batch_transfer_payload(from.clone().unwrap_or_else(|| info.sender.to_string()), recipient.to_string(), token_ids.to_vec(), amounts.to_vec())?;
    let delegate_msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };

    let mut res = Response::new().add_message(delegate_msg);

    if let Some(msg) = msg {
        let send = Cw1155BatchReceiveMsg {
            operator: info.sender.to_string(),
            from,
            batch: batch.to_vec(),
            msg,
        };
        res = res.add_message(send.into_cosmos_msg(recipient.to_string())?);
    }

    res = res.add_event(TransferEvent::new(&info.sender, &recipient, batch).into());
    Ok(res)
}

pub fn execute_approve() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn execute_approve_all(
    deps: DepsMut<EvmQueryWrapper>,
    info: MessageInfo,
    to: String,
    approved: bool,
) -> Result<Response<EvmMsg>, ContractError> {
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;

    let querier = EvmQuerier::new(&deps.querier);
    let payload = querier.erc1155_set_approval_all_payload(to.clone(), approved)?;

    let msg = EvmMsg::DelegateCallEvm { to: erc_addr, data: payload.encoded_payload };
    let event = if approved {
        ApproveAllEvent::new(&info.sender, &deps.api.addr_validate(&to)?).into()
    } else {
        RevokeAllEvent::new(&info.sender, &deps.api.addr_validate(&to)?).into()
    };

    let res = Response::new()
        .add_message(msg)
        .add_event(event);

    Ok(res)
}

pub fn execute_burn() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn execute_burn_batch() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn execute_mint() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn execute_mint_batch() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn update_ownership() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn execute_extension() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps<EvmQueryWrapper>, env: Env, msg: QueryMsg) -> Result<Binary, ContractError> {
    match msg {
        QueryMsg::BalanceOf(OwnerToken { owner, token_id }) => to_json_binary(&query_balance_of(deps, env, owner, token_id)?),
        QueryMsg::BalanceOfBatch(batch) => {
            to_json_binary(&query_balance_of_batch(deps, env, batch)?)
        },
        QueryMsg::TokenApprovals { .. } => {
            to_json_binary(&query_token_approvals()?)
        },
        QueryMsg::AllBalances { .. } => {
            to_json_binary(&query_all_balances()?)
        },
        QueryMsg::IsApprovedForAll { owner, operator } => to_json_binary(&query_is_approved_for_all(deps, env, owner, operator)?),
        QueryMsg::ApprovalsForAll {
            owner,
            include_expired: _,
            start_after,
            limit,
        } => to_json_binary(&query_all_operators(
            deps,
            env,
            owner,
            start_after,
            limit,
        )?),
        QueryMsg::NumTokens { token_id } => {
            to_json_binary(&query_num_tokens(deps, env, token_id)?)
        },
        QueryMsg::Tokens { .. } => to_json_binary(&query_tokens()?),
        QueryMsg::AllTokens { .. } => to_json_binary(&query_all_tokens()?),
        QueryMsg::Minter {} => to_json_binary(&query_minter()?),
        QueryMsg::Ownership {} => to_json_binary(&query_ownership()?),
        QueryMsg::ContractInfo {} => to_json_binary(&query_contract_info(deps, env)?),
        QueryMsg::TokenInfo { token_id } => to_json_binary(&query_nft_info(deps, env, token_id)?),
        QueryMsg::Extension { msg } => match msg {
            CwErc1155QueryMsg::EvmAddress {} => {
                to_json_binary(&ERC1155_ADDRESS.load(deps.storage)?)
            }
            CwErc1155QueryMsg::RoyaltyInfo {
                token_id,
                sale_price,
            } => to_json_binary(&query_royalty_info(deps, env, token_id, sale_price)?),
            CwErc1155QueryMsg::CheckRoyalties {} => to_json_binary(&query_check_royalties(deps, env)?),
        },
    }.map_err(Into::into)
}

pub fn query_balance_of(deps: Deps<EvmQueryWrapper>, env: Env, owner: String, token_id: String) -> StdResult<BalanceResponse> {
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let balance = Uint128::from_str(&querier.erc1155_balance_of(env.clone().contract.address.into_string(), erc_addr.clone(), owner, token_id.clone())?.balance)?;
    Ok(BalanceResponse{ balance })
}

pub fn query_balance_of_batch(deps: Deps<EvmQueryWrapper>, env: Env, batch: Vec<OwnerToken>) -> StdResult<BalancesResponse> {
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc1155_balance_of_batch(env.clone().contract.address.into_string(), erc_addr, &batch)?;
    let balances = izip!(&batch, &res.balances)
        .map(|(OwnerToken{ owner, token_id }, amount)| Balance{
            token_id: token_id.to_string(),
            owner: Addr::unchecked(owner),
            amount: amount.clone(),
        }).collect();
    Ok(BalancesResponse{ balances })
}

pub fn query_token_approvals() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn query_all_balances() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn query_is_approved_for_all(deps: Deps<EvmQueryWrapper>, env: Env, owner: String, operator: String) -> StdResult<IsApprovedForAllResponse> {
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let approved = querier.erc1155_is_approved_for_all(env.clone().contract.address.into_string(), erc_addr.clone(), owner.clone(), operator.clone())?.is_approved;
    Ok(IsApprovedForAllResponse{ approved })
}

pub fn query_contract_info(deps: Deps<EvmQueryWrapper>, env: Env) -> StdResult<ContractInfoResponse> {
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc1155_name_symbol(env.clone().contract.address.into_string(), erc_addr.clone())?;
    Ok(ContractInfoResponse{name: res.name, symbol: res.symbol})
}

pub fn query_nft_info(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
) -> StdResult<NftInfoResponse<Cw2981Extension>> {
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc1155_uri(
        env.clone().contract.address.into_string(),
        erc_addr.clone(),
        token_id.clone(),
    )?;
    let royalty_info = query_royalty_info(deps, env, token_id, 100u128.into());
    Ok(NftInfoResponse {
        token_uri: Some(res.uri),
        extension: Some(Cw2981Metadata {
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

pub fn query_tokens() -> Result<TokensResponse, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn query_all_tokens() -> Result<TokensResponse, ContractError> {
    query_tokens()
}

pub fn query_royalty_info(
    deps: Deps<EvmQueryWrapper>,
    env: Env,
    token_id: String,
    sale_price: Uint128,
) -> StdResult<RoyaltiesInfoResponse> {
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.erc1155_royalty_info(
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
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier.supports_interface(env.clone().contract.address.into_string(),erc_addr.clone(), ERC2981_ID.to_string())?;
    Ok(CheckRoyaltiesResponse {
        royalty_payments: res.supported,
    })
}

pub fn query_minter() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn query_ownership() -> Result<Response<EvmMsg>, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn query_all_operators(
    _deps: Deps<EvmQueryWrapper>,
    _env: Env,
    _owner: String,
    _start_after: Option<String>,
    _limit: Option<u32>,
) -> Result<OperatorsResponse, ContractError> {
    Err(ContractError::NotSupported {})
}

pub fn query_num_tokens(deps: Deps<EvmQueryWrapper>, env: Env, token_id: Option<String>) -> StdResult<NumTokensResponse> {
    let erc_addr = ERC1155_ADDRESS.load(deps.storage)?;
    let querier = EvmQuerier::new(&deps.querier);
    let res = querier
        .erc1155_total_supply(env.clone().contract.address.into_string(), erc_addr.clone(), token_id)?;
    Ok(NumTokensResponse {
        count: res.supply.u128() as u64,
    })
}

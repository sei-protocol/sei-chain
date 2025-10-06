#[cfg(not(feature = "library"))]
use cosmwasm_std::entry_point;
use cosmwasm_std::{
    to_json_binary, Addr, Binary, Deps, DepsMut, Env, Event, MessageInfo, Response, StdResult,
};
use sha2::{Digest, Sha256};

use crate::error::ContractError;
use crate::msg::{ExecuteMsg, InstantiateMsg, PresenceResponse, QueryMsg, ValidatorBeaconResponse};
use crate::state::{OWNER, USER_PRESENCE, VALIDATOR_BEACONS};

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    _env: Env,
    info: MessageInfo,
    msg: InstantiateMsg,
) -> Result<Response, ContractError> {
    let owner = deps.api.addr_validate(&msg.owner)?;
    OWNER.save(deps.storage, &owner)?;

    Ok(Response::new().add_event(
        Event::new("sei_mesh.owner_set")
            .add_attribute("owner", owner)
            .add_attribute("instantiated_by", info.sender),
    ))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut,
    _env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, ContractError> {
    match msg {
        ExecuteMsg::SubmitProof {
            user,
            wifi_hash,
            signed_ping,
        } => execute_submit_proof(deps, info, user, wifi_hash, signed_ping),
        ExecuteMsg::UpdateValidatorBeacon {
            validator,
            new_hash,
        } => execute_update_validator_beacon(deps, info, validator, new_hash),
    }
}

fn execute_submit_proof(
    deps: DepsMut,
    info: MessageInfo,
    user: String,
    wifi_hash: String,
    signed_ping: Binary,
) -> Result<Response, ContractError> {
    let user_addr = deps.api.addr_validate(&user)?;
    if info.sender != user_addr {
        return Err(ContractError::Unauthorized);
    }

    if !is_valid_wifi_hash(&wifi_hash) {
        return Err(ContractError::InvalidWifiHash);
    }

    if !verify_ping_signature(&user_addr, &wifi_hash, &signed_ping) {
        return Err(ContractError::InvalidSignedPing);
    }

    USER_PRESENCE.save(deps.storage, &user_addr, &wifi_hash)?;

    Ok(Response::new().add_event(
        Event::new("sei_mesh.presence_confirmed")
            .add_attribute("user", user_addr)
            .add_attribute("wifi_hash", wifi_hash),
    ))
}

fn execute_update_validator_beacon(
    deps: DepsMut,
    info: MessageInfo,
    validator: String,
    new_hash: String,
) -> Result<Response, ContractError> {
    let owner = OWNER.load(deps.storage)?;
    if info.sender != owner {
        return Err(ContractError::Unauthorized);
    }

    if !is_valid_wifi_hash(&new_hash) {
        return Err(ContractError::InvalidWifiHash);
    }

    let validator_addr = deps.api.addr_validate(&validator)?;
    VALIDATOR_BEACONS.save(deps.storage, &validator_addr, &new_hash)?;

    Ok(Response::new().add_event(
        Event::new("sei_mesh.validator_beacon_updated")
            .add_attribute("validator", validator_addr)
            .add_attribute("beacon_hash", new_hash),
    ))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Presence { user } => {
            let user_addr = deps.api.addr_validate(&user)?;
            let presence = USER_PRESENCE.may_load(deps.storage, &user_addr)?;
            to_json_binary(&PresenceResponse {
                wifi_hash: presence,
            })
        }
        QueryMsg::ValidatorBeacon { validator } => {
            let validator_addr = deps.api.addr_validate(&validator)?;
            let beacon = VALIDATOR_BEACONS.may_load(deps.storage, &validator_addr)?;
            to_json_binary(&ValidatorBeaconResponse {
                beacon_hash: beacon,
            })
        }
    }
}

fn is_valid_wifi_hash(candidate: &str) -> bool {
    candidate.len() == 64 && candidate.chars().all(|c| c.is_ascii_hexdigit())
}

fn verify_ping_signature(user: &Addr, wifi_hash: &str, signed_ping: &Binary) -> bool {
    let mut hasher = Sha256::new();
    hasher.update(user.as_bytes());
    hasher.update(wifi_hash.as_bytes());
    let expected = hasher.finalize();
    signed_ping.as_slice() == expected.as_slice()
}

#[cfg(test)]
mod tests {
    use super::*;
    use cosmwasm_std::testing::{mock_dependencies, mock_env, mock_info};
    use cosmwasm_std::{from_json, Binary};

    fn make_signature(user: &Addr, wifi_hash: &str) -> Binary {
        let mut hasher = Sha256::new();
        hasher.update(user.as_bytes());
        hasher.update(wifi_hash.as_bytes());
        Binary::from(hasher.finalize().to_vec())
    }

    #[test]
    fn instantiate_sets_owner() {
        let mut deps = mock_dependencies();
        let info = mock_info("creator", &[]);
        let msg = InstantiateMsg {
            owner: "owner1".to_string(),
        };

        let res = instantiate(deps.as_mut(), mock_env(), info, msg).unwrap();
        assert_eq!(1, res.events.len());
        assert_eq!("sei_mesh.owner_set", res.events[0].ty);
    }

    #[test]
    fn submit_proof_happy_path() {
        let mut deps = mock_dependencies();
        instantiate(
            deps.as_mut(),
            mock_env(),
            mock_info("owner", &[]),
            InstantiateMsg {
                owner: "owner".to_string(),
            },
        )
        .unwrap();

        let user_addr = deps.api.addr_validate("user1").unwrap();
        let wifi_hash = "a".repeat(64);
        let signature = make_signature(&user_addr, &wifi_hash);

        let res = execute(
            deps.as_mut(),
            mock_env(),
            mock_info(user_addr.as_str(), &[]),
            ExecuteMsg::SubmitProof {
                user: user_addr.to_string(),
                wifi_hash: wifi_hash.clone(),
                signed_ping: signature,
            },
        )
        .unwrap();

        assert_eq!("sei_mesh.presence_confirmed", res.events[0].ty);

        let query_res: PresenceResponse = from_json(
            query(
                deps.as_ref(),
                mock_env(),
                QueryMsg::Presence {
                    user: user_addr.to_string(),
                },
            )
            .unwrap(),
        )
        .unwrap();

        assert_eq!(Some(wifi_hash), query_res.wifi_hash);
    }

    #[test]
    fn submit_proof_rejects_bad_signature() {
        let mut deps = mock_dependencies();
        instantiate(
            deps.as_mut(),
            mock_env(),
            mock_info("owner", &[]),
            InstantiateMsg {
                owner: "owner".to_string(),
            },
        )
        .unwrap();

        let user_addr = deps.api.addr_validate("user1").unwrap();
        let wifi_hash = "a".repeat(64);

        let err = execute(
            deps.as_mut(),
            mock_env(),
            mock_info(user_addr.as_str(), &[]),
            ExecuteMsg::SubmitProof {
                user: user_addr.to_string(),
                wifi_hash,
                signed_ping: Binary::from(vec![0, 1, 2]),
            },
        )
        .unwrap_err();

        assert_eq!(err, ContractError::InvalidSignedPing);
    }

    #[test]
    fn update_validator_beacon_requires_owner() {
        let mut deps = mock_dependencies();
        instantiate(
            deps.as_mut(),
            mock_env(),
            mock_info("owner", &[]),
            InstantiateMsg {
                owner: "owner".to_string(),
            },
        )
        .unwrap();

        let err = execute(
            deps.as_mut(),
            mock_env(),
            mock_info("not-owner", &[]),
            ExecuteMsg::UpdateValidatorBeacon {
                validator: "validator1".to_string(),
                new_hash: "b".repeat(64),
            },
        )
        .unwrap_err();

        assert_eq!(err, ContractError::Unauthorized);
    }

    #[test]
    fn update_validator_beacon_success() {
        let mut deps = mock_dependencies();
        instantiate(
            deps.as_mut(),
            mock_env(),
            mock_info("owner", &[]),
            InstantiateMsg {
                owner: "owner".to_string(),
            },
        )
        .unwrap();

        execute(
            deps.as_mut(),
            mock_env(),
            mock_info("owner", &[]),
            ExecuteMsg::UpdateValidatorBeacon {
                validator: "validator1".to_string(),
                new_hash: "b".repeat(64),
            },
        )
        .unwrap();

        let query_res: ValidatorBeaconResponse = from_json(
            query(
                deps.as_ref(),
                mock_env(),
                QueryMsg::ValidatorBeacon {
                    validator: "validator1".to_string(),
                },
            )
            .unwrap(),
        )
        .unwrap();

        assert_eq!(Some("b".repeat(64)), query_res.beacon_hash);
    }
}

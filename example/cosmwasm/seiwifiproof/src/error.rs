use cosmwasm_std::StdError;
use thiserror::Error;

#[derive(Error, Debug, PartialEq)]
pub enum ContractError {
    #[error("Std error: {0}")]
    Std(#[from] StdError),

    #[error("unauthorized")]
    Unauthorized,

    #[error("invalid wifi hash")]
    InvalidWifiHash,

    #[error("invalid signed ping")]
    InvalidSignedPing,
}

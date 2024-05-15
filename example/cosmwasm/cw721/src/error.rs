use cosmwasm_std::StdError;
use thiserror::Error;

#[derive(Error, Debug, PartialEq)]
pub enum CwErc721ContractError {
    #[error("{0}")]
    Std(#[from] StdError),

    #[error("ERC721 does not have the requested functionality in specification")]
    NotSupported {},
}

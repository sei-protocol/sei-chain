use cosmwasm_std::{QuerierWrapper, StdResult, Uint128};

use crate::msg::{Erc20AllowanceResponse, ErcPayloadResponse, EvmQuery, EvmQueryWrapper, Route};

pub struct EvmQuerier<'a> {
    querier: &'a QuerierWrapper<'a, EvmQueryWrapper>,
}

impl<'a> EvmQuerier<'a> {
    pub fn new(querier: &'a QuerierWrapper<EvmQueryWrapper>) -> Self {
        EvmQuerier { querier }
    }

    // returns base64-encoded bytes
    pub fn erc20_transfer_payload(&self, recipient: String, amount: Uint128) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc20TransferPayload {
                recipient, amount,
            },
        }
        .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc20_transfer_from_payload(&self, owner: String, recipient: String, amount: Uint128) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc20TransferFromPayload {
                owner, recipient, amount,
            },
        }
        .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc20_approve_payload(&self, spender: String, amount: Uint128) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc20ApprovePayload {
                spender, amount,
            },
        }
        .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc20_allowance(&self, contract_address: String, owner: String, spender: String) -> StdResult<Erc20AllowanceResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc20Allowance {
                contract_address, owner, spender,
            },
        }
        .into();

        self.querier.query(&request)
    }
}
use cosmwasm_std::{QuerierWrapper, StdResult};

use crate::msg::{Route, EvmQuery, EvmQueryWrapper, ErcPayloadResponse, Erc721OwnerResponse};

pub struct EvmQuerier<'a> {
    querier: &'a QuerierWrapper<'a, EvmQueryWrapper>,
}

impl<'a> EvmQuerier<'a> {
    pub fn new(querier: &'a QuerierWrapper<EvmQueryWrapper>) -> Self {
        EvmQuerier { querier }
    }

    pub fn erc721_owner(&self, caller: String, contract_address: String, token_id: String) -> StdResult<Erc721OwnerResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721Owner { caller, contract_address, token_id },
        }
        .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc721_transfer_payload(&self, from: String, recipient: String, token_id: String) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721TransferPayload {
                from, recipient, token_id,
            },
        }
        .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc721_approve_payload(&self, spender: String, token_id: String) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721ApprovePayload {
                spender, token_id,
            },
        }
        .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc721_set_approval_all_payload(&self, to: String, approved: bool) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721SetApprovalAllPayload { to, approved, },
        }
        .into();

        self.querier.query(&request)
    }
}
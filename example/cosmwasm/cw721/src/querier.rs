use cosmwasm_std::{QuerierWrapper, StdResult};

use crate::msg::{Route, EvmQuery, EvmQueryWrapper, ErcPayloadResponse, Erc721OwnerResponse, Erc721ApprovedResponse, Erc721IsApprovedForAllResponse, Erc721TotalSupplyResponse, Erc721NameSymbolResponse, Erc721UriResponse};

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

    pub fn erc721_approved(&self, caller: String, contract_address: String, token_id: String) -> StdResult<Erc721ApprovedResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721Approved { caller, contract_address, token_id },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn erc721_is_approved_for_all(&self, caller: String, contract_address: String, owner: String, operator: String) -> StdResult<Erc721IsApprovedForAllResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721IsApprovedForAll { caller, contract_address, owner, operator },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn erc721_total_supply(&self, caller: String, contract_address: String) -> StdResult<Erc721TotalSupplyResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721TotalSupply { caller, contract_address },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn erc721_name_symbol(&self, caller: String, contract_address: String) -> StdResult<Erc721NameSymbolResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721NameSymbol { caller, contract_address },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn erc721_uri(&self, caller: String, contract_address: String, token_id: String,) -> StdResult<Erc721UriResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc721Uri { caller, contract_address, token_id },
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
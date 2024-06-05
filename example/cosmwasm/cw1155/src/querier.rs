use cosmwasm_std::{QuerierWrapper, StdResult, Uint128};
use cw1155::OwnerToken;

use crate::msg::{Route, EvmQuery, EvmQueryWrapper, ErcPayloadResponse, Erc1155BalanceOfResponse, Erc1155ApprovedResponse, Erc1155IsApprovedForAllResponse, Erc1155NameSymbolResponse, Erc1155UriResponse, Erc1155RoyaltyInfoResponse, SupportsInterfaceResponse, Erc1155TotalSupplyResponse};

pub const DEFAULT_LIMIT: u32 = 10;
pub const MAX_LIMIT: u32 = 30;

pub struct EvmQuerier<'a> {
    querier: &'a QuerierWrapper<'a, EvmQueryWrapper>,
}

impl<'a> EvmQuerier<'a> {
    pub fn new(querier: &'a QuerierWrapper<EvmQueryWrapper>) -> Self {
        EvmQuerier { querier }
    }

    pub fn erc1155_balance_of(&self, caller: String, contract_address: String, account: String, token_id: String) -> StdResult<Erc1155BalanceOfResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155BalanceOf { caller, contract_address, account, token_id },
        }
            .into();

        self.querier.query(&request)
    }

    pub fn erc1155_balance_of_batch(&self, caller: String, contract_address: String, batch: Vec<OwnerToken>) -> StdResult<Erc1155BalanceOfResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155BalanceOfBatch {
                caller,
                contract_address,
                accounts: batch.iter().map(|ot| ot.owner.to_string()).collect(),
                token_ids: batch.iter().map(|ot| ot.token_id.to_string()).collect()
            },
        }
            .into();

        self.querier.query(&request)
    }

    pub fn erc1155_approved(&self, caller: String, contract_address: String, token_id: String) -> StdResult<Erc1155ApprovedResponse> {
        // todo - is this implemented in erc1155?
        todo!()
        // let request = EvmQueryWrapper {
        //     route: Route::Evm,
        //     query_data: EvmQuery::Erc1155Approved { caller, contract_address, token_id },
        // }
        // .into();
        //
        // self.querier.query(&request)
    }

    pub fn erc1155_is_approved_for_all(&self, caller: String, contract_address: String, owner: String, operator: String) -> StdResult<Erc1155IsApprovedForAllResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155IsApprovedForAll { caller, contract_address, owner, operator },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn erc1155_name_symbol(&self, caller: String, contract_address: String) -> StdResult<Erc1155NameSymbolResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155NameSymbol { caller, contract_address },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn erc1155_uri(&self, caller: String, contract_address: String, token_id: String,) -> StdResult<Erc1155UriResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155Uri { caller, contract_address, token_id },
        }
        .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc1155_transfer_single_payload(&self, from: String, recipient: String, token_id: String, amount: Uint128) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155TransferSinglePayload {
                from, recipient, token_id, amount
            },
        }
            .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc1155_transfer_batch_payload(&self, from: String, recipient: String, token_ids: Vec<String>, amounts: Vec<Uint128>) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155TransferBatchPayload {
                from, recipient, token_ids, amounts
            },
        }
            .into();

        self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc1155_approve_payload(&self, spender: String, token_id: String) -> StdResult<ErcPayloadResponse> {
        // todo - is this implemented in erc1155?
        todo!()
        // let request = EvmQueryWrapper {
        //     route: Route::Evm,
        //     query_data: EvmQuery::Erc1155ApprovePayload {
        //         spender, token_id,
        //     },
        // }
        // .into();
        //
        // self.querier.query(&request)
    }

    // returns base64-encoded bytes
    pub fn erc1155_set_approval_all_payload(&self, to: String, approved: bool) -> StdResult<ErcPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155SetApprovalAllPayload { to, approved, },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn erc1155_royalty_info(
        &self,
        caller: String,
        contract_address: String,
        token_id: String,
        sale_price: Uint128,
    ) -> StdResult<Erc1155RoyaltyInfoResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155RoyaltyInfo {
                caller,
                contract_address,
                token_id,
                sale_price,
            },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn supports_interface(
        &self,
        caller: String,
        contract_address: String,
        interface_id: String,
    ) -> StdResult<SupportsInterfaceResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::SupportsInterface { caller, interface_id, contract_address, },
        }
        .into();

        self.querier.query(&request)
    }

    pub fn erc1155_total_supply(
        &self,
        caller: String,
        contract_address: String,
    ) -> StdResult<Erc1155TotalSupplyResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc1155TotalSupply {
                caller,
                contract_address,
            },
        }
        .into();

        self.querier.query(&request)
    }
}
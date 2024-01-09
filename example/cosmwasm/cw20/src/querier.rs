use cosmwasm_std::{QuerierWrapper, StdResult, Uint128};

use crate::msg::{Route, EvmQuery, EvmQueryWrapper, Erc20TransferPayloadResponse};

pub struct EvmQuerier<'a> {
    querier: &'a QuerierWrapper<'a, EvmQueryWrapper>,
}

impl<'a> EvmQuerier<'a> {
    pub fn new(querier: &'a QuerierWrapper<EvmQueryWrapper>) -> Self {
        EvmQuerier { querier }
    }

    // returns base64-encoded bytes
    pub fn erc20_transfer_payload(&self, recipient: String, amount: Uint128) -> StdResult<Erc20TransferPayloadResponse> {
        let request = EvmQueryWrapper {
            route: Route::Evm,
            query_data: EvmQuery::Erc20TransferPayload {
                recipient, amount,
            },
        }
        .into();

        self.querier.query(&request)
    }
}
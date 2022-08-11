use cosmwasm_std::{Addr, Decimal};
use schemars::JsonSchema;
use sei_cosmwasm::{Order};
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize, JsonSchema)]
pub struct InstantiateMsg {
    pub mars_address: Addr,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct SettlementEntry {
    pub account: String,
    pub price_denom: String,
    pub asset_denom: String,
    pub quantity: Decimal,
    pub execution_cost_or_proceed: Decimal,
    pub expected_cost_or_proceed: Decimal,
    pub position_direction: StrPositionDirection,
    pub order_type: StrOrderType,
    pub order_id: u64,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct LiquidationRequest {
    pub requestor: String,
    pub account: String,
}

#[derive(Serialize, Deserialize, Clone, PartialEq, JsonSchema)]
pub struct LiquidationResponse {
    pub successful_accounts: Vec<String>,
    pub liquidation_orders: Vec<Order>,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
pub struct DepositInfo {
    pub account: String,
    pub denom: String,
    pub amount: Decimal,
}

#[derive(Serialize, Deserialize, Clone, PartialEq, JsonSchema)]
pub struct BulkOrderPlacementsResponse {
    pub unsuccessful_orders: Vec<UnsuccessfulOrder>,
}

#[derive(Serialize, Deserialize, Copy, Clone, Debug, PartialEq, JsonSchema, Eq, Hash)]
pub enum StrPositionDirection {
    Unknown,
    Long,
    Short,
}

#[derive(Serialize, Deserialize, Copy, Clone, Debug, PartialEq, JsonSchema, Eq, Hash)]
pub enum StrOrderType {
    Unknown,
    Limit,
    Market,
    Liquidation,
}

#[derive(Serialize, Deserialize, Clone, PartialEq, JsonSchema)]
pub struct UnsuccessfulOrder {
    pub id: u64,
    pub reason: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum SudoMsg {
    Settlement {
        epoch: i64,
        entries: Vec<SettlementEntry>,
    },

    NewBlock {
        epoch: i64,
    },

    BulkOrderPlacements {
        orders: Vec<Order>,
        deposits: Vec<DepositInfo>,
    },

    BulkOrderCancellations {
        ids: Vec<u64>,
    },

    Liquidation {
        requests: Vec<LiquidationRequest>,
    },
}

use cosmwasm_std::Uint128;
use cw_storage_plus::{Item};

pub const COUNTER: Item<Uint128> = Item::new("counter");
pub const WHO: Item<String> = Item::new("who");
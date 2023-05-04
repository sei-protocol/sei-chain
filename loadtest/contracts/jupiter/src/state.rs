use cosmwasm_std::{Addr};
use cw_storage_plus::{Item};

pub const MARS_ADDR: Item<Addr> = Item::new("denoms");
use cosmwasm_std::Addr;
use cw_storage_plus::{Item, Map};

pub const OWNER: Item<Addr> = Item::new("owner");
pub const VALIDATOR_BEACONS: Map<&Addr, String> = Map::new("validator_beacons");
pub const USER_PRESENCE: Map<&Addr, String> = Map::new("user_presence");

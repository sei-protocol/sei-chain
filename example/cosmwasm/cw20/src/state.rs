use cosmwasm_std::Addr;
use cw_storage_plus::Item;

pub const ERC20_ADDRESS: Item<Addr> = Item::new("erc20_address");
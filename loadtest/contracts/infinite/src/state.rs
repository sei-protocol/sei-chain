use cosmwasm_std::{Addr, Binary, Storage};
use cw_storage_plus::{Item};
use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};
use serde::{Serialize};
use serde_json_wasm::ser::{to_vec};
use sei_cosmwasm::{Order};
use crate::msg::{DepositInfo, SettlementEntry};

pub const DOWNSTREAMS: Item<Vec<Addr>> = Item::new("downstreams");
pub const ORDER_SUM: Item<Binary> = Item::new("order");
pub const CANCEL_SUM: Item<Binary> = Item::new("cancel");
pub const SETTLEMENT_SUM: Item<Binary> = Item::new("settle");

pub fn order_placement(storage: &mut dyn Storage, orders: &Vec<Order>, deposits: &Vec<DepositInfo>) {
    let mut sum = ORDER_SUM.load(storage).unwrap();
    sum = bin_sum(sum, orders.iter().map(to_binary).collect());
    sum = bin_sum(sum, deposits.iter().map(to_binary).collect());
    ORDER_SUM.save(storage, &sum).unwrap();
}

pub fn order_cancellation(storage: &mut dyn Storage, cancellations: &Vec<u64>) {
    let mut sum = CANCEL_SUM.load(storage).unwrap();
    sum = bin_sum(sum, cancellations.iter().map(|i| -> Binary { Binary::from(i.to_ne_bytes()) }).collect());
    CANCEL_SUM.save(storage, &sum).unwrap();
}

pub fn settlement(storage: &mut dyn Storage, settlements: &Vec<SettlementEntry>) {
    let mut sum = SETTLEMENT_SUM.load(storage).unwrap();
    sum = bin_sum(sum, settlements.iter().map(to_binary).collect());
    SETTLEMENT_SUM.save(storage, &sum).unwrap();
}

fn bin_sum<T: Hash>(sum: Binary, vals: Vec<T>) -> Binary {
    let mut s = DefaultHasher::new();
    sum.hash(&mut s);
    for val in vals {
        val.hash(&mut s);
    }
    let hash = s.finish();
    Binary::from(hash.to_ne_bytes())
}

fn to_binary<T: Serialize>(t: &T) -> Binary {
    to_vec(t).map(Binary::from).unwrap()
}
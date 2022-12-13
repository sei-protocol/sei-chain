#![cfg_attr(RUSTC_WITH_SPECIALIZATION, feature(min_specialization))]
#![allow(clippy::integer_arithmetic)]
#[macro_use]
extern crate solana_bpf_loader_program;

pub mod replay;
pub mod structs;
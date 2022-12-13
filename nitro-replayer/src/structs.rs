use serde::{Serialize, Deserialize};
use solana_program::{
    pubkey::Pubkey,
    message::legacy::Message as LegacyMessage,
    message::v0::Message,
    message::v0::LoadedAddresses,
};
use solana_sdk::{
    account::{Account},
    hash::Hash,
    signature::Signature,
};

#[derive(Serialize, Deserialize)]
pub struct SlottedAccount {
    pub slot: u64,
    pub pubkey: Pubkey,
    pub account: Account,
}

#[derive(Serialize, Deserialize)]
pub struct SanitizedTransactionStruct {
    pub message: SanitizedMessageStruct,
    pub message_hash: Hash,
    pub is_simple_vote_tx: bool,
    pub signatures: Vec<Signature>,
}

#[derive(Serialize, Deserialize)]
pub struct SanitizedMessageStruct {
    pub legacy: bool,
    pub legacy_message: Option<LegacySanitizedMessage>,
    pub v0_message: Option<V0Message>,
}

#[derive(Serialize, Deserialize)]
pub struct LegacySanitizedMessage {
    pub message: LegacyMessage,
}

#[derive(Serialize, Deserialize)]
pub struct V0Message {
    pub message: Message,
    pub loaded_addresses: LoadedAddresses,
}
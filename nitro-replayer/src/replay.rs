use std::mem;
use crate::structs::SlottedAccount;
use hex::{decode, encode};
use solana_runtime::{
    message_processor::MessageProcessor,
    bank::{Bank},
    accounts_index::{AccountsIndexConfig},
    accounts_db::{AccountsDbConfig},
    transaction_error_metrics::TransactionErrorMetrics,
    rent_collector::{RentCollector},
    ancestors::Ancestors,
};
use solana_program_runtime::{
    compute_budget,
    compute_budget::ComputeBudget,
    invoke_context::{Executors},
    timings::{ExecuteTimings},
};
use solana_sdk::{
    account::{AccountSharedData, Account, ReadableAccount},
    feature_set::{
        FeatureSet,
        tx_wide_compute_cap, add_set_compute_unit_price_ix,
        requestable_heap_size,
        default_units_per_instruction,
    },
    hash::Hash,
    message::SanitizedMessage,
    transaction::{SanitizedTransaction},
    transaction_context::{TransactionContext},
    genesis_config::GenesisConfig,
    signature::Signature,
    instruction::CompiledInstruction,
};
use solana_ledger::blockstore_db::{
    LedgerColumnOptions, ShredStorageType,
};
use solana_core::validator::ValidatorConfig;
use solana_program::{
    pubkey::Pubkey,
    message::legacy::Message as LegacyMessage,
    message::MessageHeader,
};
use std::{cell::RefCell, rc::Rc, path::PathBuf, fs, collections::HashMap, io::prelude::*, slice, str};

#[repr(C)]
pub struct FilePaths {
    paths: *const ByteSliceView,
    count: usize,
}

impl FilePaths {
    pub fn from_vec(views: &[ByteSliceView]) -> Self {
        Self {
            paths: views.as_ptr(),
            count: views.len(),
        }
    }

    pub fn to_string_vec(&self) -> Vec<String> {
        let ps: &[ByteSliceView] = unsafe { slice::from_raw_parts(self.paths, self.count) };
        ps.iter().map(|p| p.to_string()).collect()
    }
}

#[repr(C)]
pub struct ByteSliceView {
    ptr: *const u8,
    len: usize,
}

impl ByteSliceView {
    pub fn from_str(string: &str) -> Self {
        let s = String::from(string);
        let s = mem::ManuallyDrop::new(s);
        Self {
            ptr: s.as_ptr(),
            len: s.len(),
        }
    }

    pub fn to_string(&self) -> String {
        let bz: &[u8] = unsafe { slice::from_raw_parts(self.ptr, self.len) };
        str::from_utf8(bz).unwrap().to_string()
    }
}

// /Users/tonychen/repos/nitro-replayer/program.txt
// /Users/tonychen/repos/nitro-replayer/owner.txt
#[no_mangle]
pub extern "C" fn replay(
    account_file_paths: FilePaths,
    sysvar_file_paths: FilePaths,
    program_file_paths: FilePaths,
    tx_file_paths: FilePaths,
    output_directory: ByteSliceView,
) {
    let accounts: Vec<SlottedAccount> = account_file_paths.to_string_vec().iter().map(|path| parse_account(path)).collect();
    let sysvar_accounts: Vec<SlottedAccount> = sysvar_file_paths.to_string_vec().iter().map(|path| parse_account(path)).collect();
    let programs: Vec<SlottedAccount> = program_file_paths.to_string_vec().iter().map(|path| parse_account(path)).collect();
    let txs: Vec<SanitizedTransaction> = tx_file_paths.to_string_vec().iter().map(|path| parse_transaction(path)).collect();
    process(&accounts, &sysvar_accounts, &programs, &txs, &output_directory.to_string());
}

fn get_genesis_config() -> GenesisConfig {
    GenesisConfig::default()
}

fn get_validator_config(genesis_config: &GenesisConfig) -> ValidatorConfig {
    let ledger_path = PathBuf::from("/Users/tonychen/solana/ledger");
    let account_paths: Vec<PathBuf> = vec![ledger_path.join("accounts")];
    let accounts_index_config = AccountsIndexConfig {
        started_from_validator: true, // this is the only place this is set
        drives: Some(vec![ledger_path.join("accounts_index")]),
        ..AccountsIndexConfig::default()
    };

    let accounts_db_config = AccountsDbConfig {
        index: Some(accounts_index_config),
        accounts_hash_cache_path: Some(ledger_path.clone()),
        ..AccountsDbConfig::default()
    };
    let accounts_db_config = Some(accounts_db_config);
    ValidatorConfig {
        account_paths: account_paths,
        accounts_hash_interval_slots: 100,
        ledger_column_options: LedgerColumnOptions {
            shred_storage_type: ShredStorageType::RocksLevel,
        },
        expected_genesis_hash: Some(genesis_config.hash()),
        voting_disabled: true,
        no_rocksdb_compaction: true,
        poh_verify: false,
        bpf_jit: true,
        no_poh_speed_test: true,
        no_os_memory_stats_reporting: true,
        no_os_network_stats_reporting: true,
        no_os_cpu_stats_reporting: true,
        accounts_db_config,
        ..ValidatorConfig::default()
    }
}

fn get_compute_budget(bank: &Bank, tx: &SanitizedTransaction) -> Option<ComputeBudget> {
    let tx_wide_compute_cap = bank.feature_set.is_active(&tx_wide_compute_cap::id());
    let compute_unit_limit = if tx_wide_compute_cap {
        compute_budget::MAX_COMPUTE_UNIT_LIMIT
    } else {
        compute_budget::DEFAULT_INSTRUCTION_COMPUTE_UNIT_LIMIT
    };
    let mut compute_budget = ComputeBudget::new(compute_unit_limit as u64);
    if tx_wide_compute_cap {
        let process_transaction_result = compute_budget.process_instructions(
            tx.message().program_instructions_iter(),
            bank.feature_set.is_active(&requestable_heap_size::id()),
            bank.feature_set.is_active(&default_units_per_instruction::id()),
            bank.feature_set.is_active(&add_set_compute_unit_price_ix::id()),
        );
        if let Err(_) = process_transaction_result {
            return None;
        }
    }
    Some(compute_budget)
}

fn process(
    accounts: &[SlottedAccount],
    sysvar_accounts: &[SlottedAccount],
    programs: &[SlottedAccount],
    sanitized_txs: &[SanitizedTransaction],
    output_directory: &str,
) {
    let genesis_config = get_genesis_config();
    let validator_config = &get_validator_config(&genesis_config);
    let bank = Bank::new_with_paths_for_replay(
        &genesis_config,
        validator_config.account_paths.clone(),
        validator_config.account_indexes.clone(),
        validator_config.accounts_db_caching_enabled,
        validator_config.accounts_shrink_ratio,
        validator_config.accounts_db_config.clone(),
        sysvar_accounts.iter().map(|slotted_account| {
            (slotted_account.pubkey.clone(), AccountSharedData::from(slotted_account.account.clone()))
        }).collect(),
    );

    let ancestors_sysvars: Vec<u64> = sysvar_accounts.iter().map(|account| account.slot).collect();
    let ancestors_accounts: Vec<u64> = accounts.iter().map(|account| account.slot).collect();
    let ancestors_programs: Vec<u64> = programs.iter().map(|account| account.slot).collect();
    let ancestors = Ancestors::from([ancestors_sysvars, ancestors_accounts, ancestors_programs].concat());
    programs.iter()
        .for_each(|slotted_account| {
            let accounts = [(
                &slotted_account.pubkey,
                &AccountSharedData::from(slotted_account.account.clone()),
            )];
            bank.rc.accounts.accounts_db.store_uncached(slotted_account.slot, &accounts);
        });
    accounts.iter()
        .for_each(|slotted_account| {
            let accounts = [(
                &slotted_account.pubkey,
                &AccountSharedData::from(slotted_account.account.clone()),
            )];
            bank.rc.accounts.accounts_db.store_uncached(slotted_account.slot, &accounts);
        });

    let mut new_account_data: HashMap<Pubkey, AccountSharedData> = HashMap::new();
    sanitized_txs.iter().for_each(|sanitized_tx| {
        let lock_results = bank.rc.accounts.lock_accounts([sanitized_tx.clone()].iter(), &FeatureSet::all_enabled());
        match sanitized_tx.message().clone() {
            SanitizedMessage::Legacy(msg) => {
                bank.blockhash_queue.write().unwrap().register_hash(&msg.recent_blockhash, bank.get_lamports_per_signature());
            },
            SanitizedMessage::V0(msg) => {
                bank.blockhash_queue.write().unwrap().register_hash(&msg.message.recent_blockhash, bank.get_lamports_per_signature());
            },
        }
        let mut error_counters = TransactionErrorMetrics::default();
        let check_results = bank.check_transactions(
            &[sanitized_tx.clone()],
            &lock_results,
            usize::MAX,
            &mut error_counters,
        );
        let rent_collector = RentCollector::default();
        let mut loaded_transactions = bank.rc.accounts.load_accounts(
            &ancestors,
            &[sanitized_tx.clone()],
            check_results,
            &bank.blockhash_queue.read().unwrap(),
            &mut error_counters,
            &rent_collector,
            &bank.feature_set,
            &bank.fee_structure,
            None,
        );
        loaded_transactions
            .iter_mut()
            .zip(sanitized_txs.iter())
            .for_each(|(accs, tx)| match accs {
                (Err(e), _nonce) => {
                    panic!("{}", e);
                },
                (Ok(loaded_transaction), _) => {
                    let compute_budget = get_compute_budget(&bank, tx).unwrap();

                    let executors = Rc::new(RefCell::new(Executors::default()));

                    let mut transaction_accounts = Vec::new();
                    std::mem::swap(&mut loaded_transaction.accounts, &mut transaction_accounts);
                    let mut transaction_context = TransactionContext::new(
                        transaction_accounts,
                        compute_budget.max_invoke_depth.saturating_add(1),
                        tx.message().instructions().len(),
                    );
                    let (blockhash, lamports_per_signature) = bank.last_blockhash_and_lamports_per_signature();

                    let mut executed_units = 0u64;

                    MessageProcessor::process_message(
                        &bank.builtin_programs.vec,
                        tx.message(),
                        &loaded_transaction.program_indices,
                        &mut transaction_context,
                        rent_collector.rent,
                        None,
                        executors.clone(),
                        bank.feature_set.clone(),
                        compute_budget,
                        &mut ExecuteTimings::default(),
                        &*bank.sysvar_cache.read().unwrap(),
                        blockhash,
                        lamports_per_signature,
                        bank.load_accounts_data_size(),
                        &mut executed_units,
                    ).expect("process failed");

                    let (accounts, _) = transaction_context.deconstruct();
                    accounts.iter().for_each(|(pk, account)| {
                        new_account_data.insert(pk.clone(), account.clone());
                    });
                }
            });
        bank.rc
            .accounts
            .unlock_accounts([sanitized_tx.clone()].iter(), &lock_results);
    });

    new_account_data.iter().for_each(|(k, v)| {
        let mut f = fs::File::create(format!("{}/{}", output_directory, k.to_string())).expect("failed to open file");
        f.write(serialize_account_data(k, v).as_bytes()).expect("failed to write");
    });
}

fn parse_account(filepath: &str) -> SlottedAccount {
    let content = fs::read_to_string(filepath).expect("failed to read file");
    let parts: Vec<&str> = content.split("\n").map(|part| part.trim() ).collect();
    let target = parts[0];
    let cols: Vec<&str> = target.split("|").map(|col| col.trim()).collect();
    SlottedAccount {
        slot: cols[3].parse::<u64>().expect("parse error"),
        pubkey: Pubkey::new(
            &decode(&cols[0]).expect("Decoding failed"),
        ),
        account: Account {
            lamports: cols[2].parse::<u64>().expect("parse error"),
            data: decode(&cols[6]).expect("Decoding failed"),
            owner: Pubkey::new(
                &decode(&cols[1]).expect("Decoding failed"),
            ),
            executable: cols[4] == "t",
            rent_epoch: cols[5].parse::<u64>().expect("parse error"),
        },
    }
}

fn parse_transaction(filepath: &str) -> SanitizedTransaction {
    let content = fs::read_to_string(filepath).expect("failed to read file");
    println!("{}", &content);
    let parts: Vec<&str> = content.split("\n").map(|part| part.trim() ).collect();
    let target = parts[0];
    let cols: Vec<&str> = target.split("|").map(|col| col.trim()).collect();
    if cols[0] == "0" {
        let msg_parts: Vec<&str> = cols[1].split(",").collect();
        let header_parts: Vec<&str> = msg_parts[0].split("-").collect();
        let account_key_parts: Vec<&str> = msg_parts[1].split("-").collect();
        let recent_blockhash = msg_parts[2].clone();
        let instructions_parts = msg_parts[3].clone();
        SanitizedTransaction {
            message_hash: Hash::new(&decode(cols[2]).expect("Decoding failed")),
            is_simple_vote_tx: cols[3] == "t",
            signatures: cols[4].split(",").map(|sig| Signature::new(&decode(sig).expect("bad sig"))).collect(),
            message: SanitizedMessage::Legacy(
                LegacyMessage {
                    header: MessageHeader{
                        num_required_signatures: header_parts[0].parse::<u8>().expect("parse error"),
                        num_readonly_signed_accounts: header_parts[1].parse::<u8>().expect("parse error"),
                        num_readonly_unsigned_accounts: header_parts[2].parse::<u8>().expect("parse error"),
                    },
                    account_keys: account_key_parts.iter().map(|key| Pubkey::new(
                        &decode(*key).expect("Decoding failed"),
                    )).collect(),
                    recent_blockhash: Hash::new(&decode(recent_blockhash).expect("Decoding failed")),
                    instructions: instructions_parts.split("-").map(parse_instruction).collect(),
                },
            ),
        }
    } else {
        panic!("not supported")
    }
}

fn parse_instruction(instruction: &str) -> CompiledInstruction {
    let instruction_parts: Vec<&str> = instruction.split("_").collect();
    CompiledInstruction {
        program_id_index: instruction_parts[0].parse::<u8>().expect("parse error"),
        accounts: instruction_parts[1].split(":").map(|a| a.parse::<u8>().expect("parse error")).collect(),
        data: decode(instruction_parts[2]).expect("Decoding failed"),
    }
}

fn serialize_account_data(pubkey: &Pubkey, data: &AccountSharedData) -> String {
    format!(
        "{}|{}|{}|{}|{}|{}",
        pubkey.to_string(),
        data.owner(),
        data.lamports(),
        data.executable(),
        data.rent_epoch(),
        encode(data.data()),
    )
}

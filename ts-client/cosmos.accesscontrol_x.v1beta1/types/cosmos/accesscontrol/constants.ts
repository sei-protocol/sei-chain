/* eslint-disable */
export const protobufPackage = "cosmos.accesscontrol.v1beta1";

export enum AccessType {
  UNKNOWN = 0,
  READ = 1,
  WRITE = 2,
  COMMIT = 3,
  UNRECOGNIZED = -1,
}

export function accessTypeFromJSON(object: any): AccessType {
  switch (object) {
    case 0:
    case "UNKNOWN":
      return AccessType.UNKNOWN;
    case 1:
    case "READ":
      return AccessType.READ;
    case 2:
    case "WRITE":
      return AccessType.WRITE;
    case 3:
    case "COMMIT":
      return AccessType.COMMIT;
    case -1:
    case "UNRECOGNIZED":
    default:
      return AccessType.UNRECOGNIZED;
  }
}

export function accessTypeToJSON(object: AccessType): string {
  switch (object) {
    case AccessType.UNKNOWN:
      return "UNKNOWN";
    case AccessType.READ:
      return "READ";
    case AccessType.WRITE:
      return "WRITE";
    case AccessType.COMMIT:
      return "COMMIT";
    default:
      return "UNKNOWN";
  }
}

export enum AccessOperationSelectorType {
  NONE = 0,
  JQ = 1,
  JQ_BECH32_ADDRESS = 2,
  JQ_LENGTH_PREFIXED_ADDRESS = 3,
  SENDER_BECH32_ADDRESS = 4,
  SENDER_LENGTH_PREFIXED_ADDRESS = 5,
  CONTRACT_ADDRESS = 6,
  JQ_MESSAGE_CONDITIONAL = 7,
  CONSTANT_STRING_TO_HEX = 8,
  CONTRACT_REFERENCE = 9,
  UNRECOGNIZED = -1,
}

export function accessOperationSelectorTypeFromJSON(
  object: any
): AccessOperationSelectorType {
  switch (object) {
    case 0:
    case "NONE":
      return AccessOperationSelectorType.NONE;
    case 1:
    case "JQ":
      return AccessOperationSelectorType.JQ;
    case 2:
    case "JQ_BECH32_ADDRESS":
      return AccessOperationSelectorType.JQ_BECH32_ADDRESS;
    case 3:
    case "JQ_LENGTH_PREFIXED_ADDRESS":
      return AccessOperationSelectorType.JQ_LENGTH_PREFIXED_ADDRESS;
    case 4:
    case "SENDER_BECH32_ADDRESS":
      return AccessOperationSelectorType.SENDER_BECH32_ADDRESS;
    case 5:
    case "SENDER_LENGTH_PREFIXED_ADDRESS":
      return AccessOperationSelectorType.SENDER_LENGTH_PREFIXED_ADDRESS;
    case 6:
    case "CONTRACT_ADDRESS":
      return AccessOperationSelectorType.CONTRACT_ADDRESS;
    case 7:
    case "JQ_MESSAGE_CONDITIONAL":
      return AccessOperationSelectorType.JQ_MESSAGE_CONDITIONAL;
    case 8:
    case "CONSTANT_STRING_TO_HEX":
      return AccessOperationSelectorType.CONSTANT_STRING_TO_HEX;
    case 9:
    case "CONTRACT_REFERENCE":
      return AccessOperationSelectorType.CONTRACT_REFERENCE;
    case -1:
    case "UNRECOGNIZED":
    default:
      return AccessOperationSelectorType.UNRECOGNIZED;
  }
}

export function accessOperationSelectorTypeToJSON(
  object: AccessOperationSelectorType
): string {
  switch (object) {
    case AccessOperationSelectorType.NONE:
      return "NONE";
    case AccessOperationSelectorType.JQ:
      return "JQ";
    case AccessOperationSelectorType.JQ_BECH32_ADDRESS:
      return "JQ_BECH32_ADDRESS";
    case AccessOperationSelectorType.JQ_LENGTH_PREFIXED_ADDRESS:
      return "JQ_LENGTH_PREFIXED_ADDRESS";
    case AccessOperationSelectorType.SENDER_BECH32_ADDRESS:
      return "SENDER_BECH32_ADDRESS";
    case AccessOperationSelectorType.SENDER_LENGTH_PREFIXED_ADDRESS:
      return "SENDER_LENGTH_PREFIXED_ADDRESS";
    case AccessOperationSelectorType.CONTRACT_ADDRESS:
      return "CONTRACT_ADDRESS";
    case AccessOperationSelectorType.JQ_MESSAGE_CONDITIONAL:
      return "JQ_MESSAGE_CONDITIONAL";
    case AccessOperationSelectorType.CONSTANT_STRING_TO_HEX:
      return "CONSTANT_STRING_TO_HEX";
    case AccessOperationSelectorType.CONTRACT_REFERENCE:
      return "CONTRACT_REFERENCE";
    default:
      return "UNKNOWN";
  }
}

export enum ResourceType {
  ANY = 0,
  /** KV - child of ANY */
  KV = 1,
  /** Mem - child of ANY */
  Mem = 2,
  /** DexMem - child of MEM */
  DexMem = 3,
  /** KV_BANK - child of KV */
  KV_BANK = 4,
  /** KV_STAKING - child of KV */
  KV_STAKING = 5,
  /** KV_WASM - child of KV */
  KV_WASM = 6,
  /** KV_ORACLE - child of KV */
  KV_ORACLE = 7,
  /** KV_DEX - child of KV */
  KV_DEX = 8,
  /** KV_EPOCH - child of KV */
  KV_EPOCH = 9,
  /** KV_TOKENFACTORY - child of KV */
  KV_TOKENFACTORY = 10,
  /** KV_ORACLE_VOTE_TARGETS - child of KV_ORACLE */
  KV_ORACLE_VOTE_TARGETS = 11,
  /** KV_ORACLE_AGGREGATE_VOTES - child of KV_ORACLE */
  KV_ORACLE_AGGREGATE_VOTES = 12,
  /** KV_ORACLE_FEEDERS - child of KV_ORACLE */
  KV_ORACLE_FEEDERS = 13,
  /** KV_STAKING_DELEGATION - child of KV_STAKING */
  KV_STAKING_DELEGATION = 14,
  /** KV_STAKING_VALIDATOR - child of KV_STAKING */
  KV_STAKING_VALIDATOR = 15,
  /** KV_AUTH - child of KV */
  KV_AUTH = 16,
  /** KV_AUTH_ADDRESS_STORE - child of KV */
  KV_AUTH_ADDRESS_STORE = 17,
  /** KV_BANK_SUPPLY - child of KV_BANK */
  KV_BANK_SUPPLY = 18,
  /** KV_BANK_DENOM - child of KV_BANK */
  KV_BANK_DENOM = 19,
  /** KV_BANK_BALANCES - child of KV_BANK */
  KV_BANK_BALANCES = 20,
  /** KV_TOKENFACTORY_DENOM - child of KV_TOKENFACTORY */
  KV_TOKENFACTORY_DENOM = 21,
  /** KV_TOKENFACTORY_METADATA - child of KV_TOKENFACTORY */
  KV_TOKENFACTORY_METADATA = 22,
  /** KV_TOKENFACTORY_ADMIN - child of KV_TOKENFACTORY */
  KV_TOKENFACTORY_ADMIN = 23,
  /** KV_TOKENFACTORY_CREATOR - child of KV_TOKENFACTORY */
  KV_TOKENFACTORY_CREATOR = 24,
  /** KV_ORACLE_EXCHANGE_RATE - child of KV_ORACLE */
  KV_ORACLE_EXCHANGE_RATE = 25,
  /** KV_ORACLE_VOTE_PENALTY_COUNTER - child of KV_ORACLE */
  KV_ORACLE_VOTE_PENALTY_COUNTER = 26,
  /** KV_ORACLE_PRICE_SNAPSHOT - child of KV_ORACLE */
  KV_ORACLE_PRICE_SNAPSHOT = 27,
  /** KV_STAKING_VALIDATION_POWER - child of KV_STAKING */
  KV_STAKING_VALIDATION_POWER = 28,
  /** KV_STAKING_TOTAL_POWER - child of KV_STAKING */
  KV_STAKING_TOTAL_POWER = 29,
  /** KV_STAKING_VALIDATORS_CON_ADDR - child of KV_STAKING */
  KV_STAKING_VALIDATORS_CON_ADDR = 30,
  /** KV_STAKING_UNBONDING_DELEGATION - child of KV_STAKING */
  KV_STAKING_UNBONDING_DELEGATION = 31,
  /** KV_STAKING_UNBONDING_DELEGATION_VAL - child of KV_STAKING */
  KV_STAKING_UNBONDING_DELEGATION_VAL = 32,
  /** KV_STAKING_REDELEGATION - child of KV_STAKING */
  KV_STAKING_REDELEGATION = 33,
  /** KV_STAKING_REDELEGATION_VAL_SRC - child of KV_STAKING */
  KV_STAKING_REDELEGATION_VAL_SRC = 34,
  /** KV_STAKING_REDELEGATION_VAL_DST - child of KV_STAKING */
  KV_STAKING_REDELEGATION_VAL_DST = 35,
  /** KV_STAKING_REDELEGATION_QUEUE - child of KV_STAKING */
  KV_STAKING_REDELEGATION_QUEUE = 36,
  /** KV_STAKING_VALIDATOR_QUEUE - child of KV_STAKING */
  KV_STAKING_VALIDATOR_QUEUE = 37,
  /** KV_STAKING_HISTORICAL_INFO - child of KV_STAKING */
  KV_STAKING_HISTORICAL_INFO = 38,
  /** KV_STAKING_UNBONDING - child of KV_STAKING */
  KV_STAKING_UNBONDING = 39,
  /** KV_STAKING_VALIDATORS_BY_POWER - child of KV_STAKING */
  KV_STAKING_VALIDATORS_BY_POWER = 41,
  /** KV_DISTRIBUTION - child of KV */
  KV_DISTRIBUTION = 40,
  /** KV_DISTRIBUTION_FEE_POOL - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_FEE_POOL = 42,
  /** KV_DISTRIBUTION_PROPOSER_KEY - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_PROPOSER_KEY = 43,
  /** KV_DISTRIBUTION_OUTSTANDING_REWARDS - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_OUTSTANDING_REWARDS = 44,
  /** KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR = 45,
  /** KV_DISTRIBUTION_DELEGATOR_STARTING_INFO - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_DELEGATOR_STARTING_INFO = 46,
  /** KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS = 47,
  /** KV_DISTRIBUTION_VAL_CURRENT_REWARDS - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_VAL_CURRENT_REWARDS = 48,
  /** KV_DISTRIBUTION_VAL_ACCUM_COMMISSION - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_VAL_ACCUM_COMMISSION = 49,
  /** KV_DISTRIBUTION_SLASH_EVENT - child of KV_DISTRIBUTION */
  KV_DISTRIBUTION_SLASH_EVENT = 50,
  /** KV_DEX_CONTRACT_LONGBOOK - child of KV_DEX */
  KV_DEX_CONTRACT_LONGBOOK = 51,
  /** KV_DEX_CONTRACT_SHORTBOOK - child of KV_DEX */
  KV_DEX_CONTRACT_SHORTBOOK = 52,
  /** KV_DEX_SETTLEMENT - child of KV_DEX */
  KV_DEX_SETTLEMENT = 53,
  /** KV_DEX_PAIR_PREFIX - child of KV_DEX */
  KV_DEX_PAIR_PREFIX = 54,
  /** KV_DEX_TWAP - child of KV_DEX */
  KV_DEX_TWAP = 55,
  /** KV_DEX_PRICE - child of KV_DEX */
  KV_DEX_PRICE = 56,
  /** KV_DEX_SETTLEMENT_ENTRY - child of KV_DEX */
  KV_DEX_SETTLEMENT_ENTRY = 57,
  /** KV_DEX_REGISTERED_PAIR - child of KV_DEX */
  KV_DEX_REGISTERED_PAIR = 58,
  /** KV_DEX_ORDER - child of KV_DEX */
  KV_DEX_ORDER = 60,
  /** KV_DEX_CANCEL - child of KV_DEX */
  KV_DEX_CANCEL = 61,
  /** KV_DEX_ACCOUNT_ACTIVE_ORDERS - child of KV_DEX */
  KV_DEX_ACCOUNT_ACTIVE_ORDERS = 62,
  /** KV_DEX_ASSET_LIST - child of KV_DEX */
  KV_DEX_ASSET_LIST = 64,
  /** KV_DEX_NEXT_ORDER_ID - child of KV_DEX */
  KV_DEX_NEXT_ORDER_ID = 65,
  /** KV_DEX_NEXT_SETTLEMENT_ID - child of KV_DEX */
  KV_DEX_NEXT_SETTLEMENT_ID = 66,
  /** KV_DEX_MATCH_RESULT - child of KV_DEX */
  KV_DEX_MATCH_RESULT = 67,
  /** KV_DEX_SETTLEMENT_ORDER_ID - child of KV_DEX */
  KV_DEX_SETTLEMENT_ORDER_ID = 68,
  /** KV_DEX_ORDER_BOOK - child of KV_DEX */
  KV_DEX_ORDER_BOOK = 69,
  /** KV_ACCESSCONTROL - child of KV */
  KV_ACCESSCONTROL = 71,
  /** KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING - child of KV_ACCESSCONTROL */
  KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING = 72,
  /** KV_WASM_CODE - child of KV_WASM */
  KV_WASM_CODE = 73,
  /** KV_WASM_CONTRACT_ADDRESS - child of KV_WASM */
  KV_WASM_CONTRACT_ADDRESS = 74,
  /** KV_WASM_CONTRACT_STORE - child of KV_WASM */
  KV_WASM_CONTRACT_STORE = 75,
  /** KV_WASM_SEQUENCE_KEY - child of KV_WASM */
  KV_WASM_SEQUENCE_KEY = 76,
  /** KV_WASM_CONTRACT_CODE_HISTORY - child of KV_WASM */
  KV_WASM_CONTRACT_CODE_HISTORY = 77,
  /** KV_WASM_CONTRACT_BY_CODE_ID - child of KV_WASM */
  KV_WASM_CONTRACT_BY_CODE_ID = 78,
  /** KV_WASM_PINNED_CODE_INDEX - child of KV_WASM */
  KV_WASM_PINNED_CODE_INDEX = 79,
  /** KV_AUTH_GLOBAL_ACCOUNT_NUMBER - child of KV_AUTH */
  KV_AUTH_GLOBAL_ACCOUNT_NUMBER = 80,
  /** KV_AUTHZ - child of KV */
  KV_AUTHZ = 81,
  /** KV_FEEGRANT - child of KV */
  KV_FEEGRANT = 82,
  /** KV_FEEGRANT_ALLOWANCE - child of KV_FEEGRANT */
  KV_FEEGRANT_ALLOWANCE = 83,
  /** KV_SLASHING - child of KV */
  KV_SLASHING = 84,
  /** KV_SLASHING_VAL_SIGNING_INFO - child of KV_SLASHING */
  KV_SLASHING_VAL_SIGNING_INFO = 85,
  /** KV_SLASHING_ADDR_PUBKEY_RELATION_KEY - child of KV_SLASHING */
  KV_SLASHING_ADDR_PUBKEY_RELATION_KEY = 86,
  KV_DEX_MEM_ORDER = 87,
  KV_DEX_MEM_CANCEL = 88,
  KV_DEX_MEM_DEPOSIT = 89,
  /** KV_DEX_CONTRACT - child of KV_DEX */
  KV_DEX_CONTRACT = 90,
  /** KV_DEX_LONG_ORDER_COUNT - child of KV_DEX */
  KV_DEX_LONG_ORDER_COUNT = 91,
  /** KV_DEX_SHORT_ORDER_COUNT - child of KV_DEX */
  KV_DEX_SHORT_ORDER_COUNT = 92,
  /** KV_BANK_DEFERRED - child of KV */
  KV_BANK_DEFERRED = 93,
  /** KV_BANK_DEFERRED_MODULE_TX_INDEX - child of KV_BANK_DEFERRED */
  KV_BANK_DEFERRED_MODULE_TX_INDEX = 95,
  /** KV_EVM - child of KV */
  KV_EVM = 96,
  /** KV_EVM_BALANCE - child of KV_EVM; deprecated */
  KV_EVM_BALANCE = 97,
  /** KV_EVM_TRANSIENT - child of KV_EVM */
  KV_EVM_TRANSIENT = 98,
  /** KV_EVM_ACCOUNT_TRANSIENT - child of KV_EVM */
  KV_EVM_ACCOUNT_TRANSIENT = 99,
  /** KV_EVM_MODULE_TRANSIENT - child of KV_EVM */
  KV_EVM_MODULE_TRANSIENT = 100,
  /** KV_EVM_NONCE - child of KV_EVM */
  KV_EVM_NONCE = 101,
  /** KV_EVM_RECEIPT - child of KV_EVM */
  KV_EVM_RECEIPT = 102,
  /** KV_EVM_S2E - child of KV_EVM */
  KV_EVM_S2E = 103,
  /** KV_EVM_E2S - child of KV_EVM */
  KV_EVM_E2S = 104,
  /** KV_EVM_CODE_HASH - child of KV_EVM */
  KV_EVM_CODE_HASH = 105,
  /** KV_EVM_CODE - child of KV_EVM */
  KV_EVM_CODE = 106,
  /** KV_EVM_CODE_SIZE - child of KV_EVM */
  KV_EVM_CODE_SIZE = 107,
  /** KV_BANK_WEI_BALANCE - child of KV_BANK */
  KV_BANK_WEI_BALANCE = 108,
  /** KV_DEX_MEM_CONTRACTS_TO_PROCESS - child of KV_DEX_MEM */
  KV_DEX_MEM_CONTRACTS_TO_PROCESS = 109,
  /** KV_DEX_MEM_DOWNSTREAM_CONTRACTS - child of KV_DEX_MEM */
  KV_DEX_MEM_DOWNSTREAM_CONTRACTS = 110,
  UNRECOGNIZED = -1,
}

export function resourceTypeFromJSON(object: any): ResourceType {
  switch (object) {
    case 0:
    case "ANY":
      return ResourceType.ANY;
    case 1:
    case "KV":
      return ResourceType.KV;
    case 2:
    case "Mem":
      return ResourceType.Mem;
    case 3:
    case "DexMem":
      return ResourceType.DexMem;
    case 4:
    case "KV_BANK":
      return ResourceType.KV_BANK;
    case 5:
    case "KV_STAKING":
      return ResourceType.KV_STAKING;
    case 6:
    case "KV_WASM":
      return ResourceType.KV_WASM;
    case 7:
    case "KV_ORACLE":
      return ResourceType.KV_ORACLE;
    case 8:
    case "KV_DEX":
      return ResourceType.KV_DEX;
    case 9:
    case "KV_EPOCH":
      return ResourceType.KV_EPOCH;
    case 10:
    case "KV_TOKENFACTORY":
      return ResourceType.KV_TOKENFACTORY;
    case 11:
    case "KV_ORACLE_VOTE_TARGETS":
      return ResourceType.KV_ORACLE_VOTE_TARGETS;
    case 12:
    case "KV_ORACLE_AGGREGATE_VOTES":
      return ResourceType.KV_ORACLE_AGGREGATE_VOTES;
    case 13:
    case "KV_ORACLE_FEEDERS":
      return ResourceType.KV_ORACLE_FEEDERS;
    case 14:
    case "KV_STAKING_DELEGATION":
      return ResourceType.KV_STAKING_DELEGATION;
    case 15:
    case "KV_STAKING_VALIDATOR":
      return ResourceType.KV_STAKING_VALIDATOR;
    case 16:
    case "KV_AUTH":
      return ResourceType.KV_AUTH;
    case 17:
    case "KV_AUTH_ADDRESS_STORE":
      return ResourceType.KV_AUTH_ADDRESS_STORE;
    case 18:
    case "KV_BANK_SUPPLY":
      return ResourceType.KV_BANK_SUPPLY;
    case 19:
    case "KV_BANK_DENOM":
      return ResourceType.KV_BANK_DENOM;
    case 20:
    case "KV_BANK_BALANCES":
      return ResourceType.KV_BANK_BALANCES;
    case 21:
    case "KV_TOKENFACTORY_DENOM":
      return ResourceType.KV_TOKENFACTORY_DENOM;
    case 22:
    case "KV_TOKENFACTORY_METADATA":
      return ResourceType.KV_TOKENFACTORY_METADATA;
    case 23:
    case "KV_TOKENFACTORY_ADMIN":
      return ResourceType.KV_TOKENFACTORY_ADMIN;
    case 24:
    case "KV_TOKENFACTORY_CREATOR":
      return ResourceType.KV_TOKENFACTORY_CREATOR;
    case 25:
    case "KV_ORACLE_EXCHANGE_RATE":
      return ResourceType.KV_ORACLE_EXCHANGE_RATE;
    case 26:
    case "KV_ORACLE_VOTE_PENALTY_COUNTER":
      return ResourceType.KV_ORACLE_VOTE_PENALTY_COUNTER;
    case 27:
    case "KV_ORACLE_PRICE_SNAPSHOT":
      return ResourceType.KV_ORACLE_PRICE_SNAPSHOT;
    case 28:
    case "KV_STAKING_VALIDATION_POWER":
      return ResourceType.KV_STAKING_VALIDATION_POWER;
    case 29:
    case "KV_STAKING_TOTAL_POWER":
      return ResourceType.KV_STAKING_TOTAL_POWER;
    case 30:
    case "KV_STAKING_VALIDATORS_CON_ADDR":
      return ResourceType.KV_STAKING_VALIDATORS_CON_ADDR;
    case 31:
    case "KV_STAKING_UNBONDING_DELEGATION":
      return ResourceType.KV_STAKING_UNBONDING_DELEGATION;
    case 32:
    case "KV_STAKING_UNBONDING_DELEGATION_VAL":
      return ResourceType.KV_STAKING_UNBONDING_DELEGATION_VAL;
    case 33:
    case "KV_STAKING_REDELEGATION":
      return ResourceType.KV_STAKING_REDELEGATION;
    case 34:
    case "KV_STAKING_REDELEGATION_VAL_SRC":
      return ResourceType.KV_STAKING_REDELEGATION_VAL_SRC;
    case 35:
    case "KV_STAKING_REDELEGATION_VAL_DST":
      return ResourceType.KV_STAKING_REDELEGATION_VAL_DST;
    case 36:
    case "KV_STAKING_REDELEGATION_QUEUE":
      return ResourceType.KV_STAKING_REDELEGATION_QUEUE;
    case 37:
    case "KV_STAKING_VALIDATOR_QUEUE":
      return ResourceType.KV_STAKING_VALIDATOR_QUEUE;
    case 38:
    case "KV_STAKING_HISTORICAL_INFO":
      return ResourceType.KV_STAKING_HISTORICAL_INFO;
    case 39:
    case "KV_STAKING_UNBONDING":
      return ResourceType.KV_STAKING_UNBONDING;
    case 41:
    case "KV_STAKING_VALIDATORS_BY_POWER":
      return ResourceType.KV_STAKING_VALIDATORS_BY_POWER;
    case 40:
    case "KV_DISTRIBUTION":
      return ResourceType.KV_DISTRIBUTION;
    case 42:
    case "KV_DISTRIBUTION_FEE_POOL":
      return ResourceType.KV_DISTRIBUTION_FEE_POOL;
    case 43:
    case "KV_DISTRIBUTION_PROPOSER_KEY":
      return ResourceType.KV_DISTRIBUTION_PROPOSER_KEY;
    case 44:
    case "KV_DISTRIBUTION_OUTSTANDING_REWARDS":
      return ResourceType.KV_DISTRIBUTION_OUTSTANDING_REWARDS;
    case 45:
    case "KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR":
      return ResourceType.KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR;
    case 46:
    case "KV_DISTRIBUTION_DELEGATOR_STARTING_INFO":
      return ResourceType.KV_DISTRIBUTION_DELEGATOR_STARTING_INFO;
    case 47:
    case "KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS":
      return ResourceType.KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS;
    case 48:
    case "KV_DISTRIBUTION_VAL_CURRENT_REWARDS":
      return ResourceType.KV_DISTRIBUTION_VAL_CURRENT_REWARDS;
    case 49:
    case "KV_DISTRIBUTION_VAL_ACCUM_COMMISSION":
      return ResourceType.KV_DISTRIBUTION_VAL_ACCUM_COMMISSION;
    case 50:
    case "KV_DISTRIBUTION_SLASH_EVENT":
      return ResourceType.KV_DISTRIBUTION_SLASH_EVENT;
    case 51:
    case "KV_DEX_CONTRACT_LONGBOOK":
      return ResourceType.KV_DEX_CONTRACT_LONGBOOK;
    case 52:
    case "KV_DEX_CONTRACT_SHORTBOOK":
      return ResourceType.KV_DEX_CONTRACT_SHORTBOOK;
    case 53:
    case "KV_DEX_SETTLEMENT":
      return ResourceType.KV_DEX_SETTLEMENT;
    case 54:
    case "KV_DEX_PAIR_PREFIX":
      return ResourceType.KV_DEX_PAIR_PREFIX;
    case 55:
    case "KV_DEX_TWAP":
      return ResourceType.KV_DEX_TWAP;
    case 56:
    case "KV_DEX_PRICE":
      return ResourceType.KV_DEX_PRICE;
    case 57:
    case "KV_DEX_SETTLEMENT_ENTRY":
      return ResourceType.KV_DEX_SETTLEMENT_ENTRY;
    case 58:
    case "KV_DEX_REGISTERED_PAIR":
      return ResourceType.KV_DEX_REGISTERED_PAIR;
    case 60:
    case "KV_DEX_ORDER":
      return ResourceType.KV_DEX_ORDER;
    case 61:
    case "KV_DEX_CANCEL":
      return ResourceType.KV_DEX_CANCEL;
    case 62:
    case "KV_DEX_ACCOUNT_ACTIVE_ORDERS":
      return ResourceType.KV_DEX_ACCOUNT_ACTIVE_ORDERS;
    case 64:
    case "KV_DEX_ASSET_LIST":
      return ResourceType.KV_DEX_ASSET_LIST;
    case 65:
    case "KV_DEX_NEXT_ORDER_ID":
      return ResourceType.KV_DEX_NEXT_ORDER_ID;
    case 66:
    case "KV_DEX_NEXT_SETTLEMENT_ID":
      return ResourceType.KV_DEX_NEXT_SETTLEMENT_ID;
    case 67:
    case "KV_DEX_MATCH_RESULT":
      return ResourceType.KV_DEX_MATCH_RESULT;
    case 68:
    case "KV_DEX_SETTLEMENT_ORDER_ID":
      return ResourceType.KV_DEX_SETTLEMENT_ORDER_ID;
    case 69:
    case "KV_DEX_ORDER_BOOK":
      return ResourceType.KV_DEX_ORDER_BOOK;
    case 71:
    case "KV_ACCESSCONTROL":
      return ResourceType.KV_ACCESSCONTROL;
    case 72:
    case "KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING":
      return ResourceType.KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING;
    case 73:
    case "KV_WASM_CODE":
      return ResourceType.KV_WASM_CODE;
    case 74:
    case "KV_WASM_CONTRACT_ADDRESS":
      return ResourceType.KV_WASM_CONTRACT_ADDRESS;
    case 75:
    case "KV_WASM_CONTRACT_STORE":
      return ResourceType.KV_WASM_CONTRACT_STORE;
    case 76:
    case "KV_WASM_SEQUENCE_KEY":
      return ResourceType.KV_WASM_SEQUENCE_KEY;
    case 77:
    case "KV_WASM_CONTRACT_CODE_HISTORY":
      return ResourceType.KV_WASM_CONTRACT_CODE_HISTORY;
    case 78:
    case "KV_WASM_CONTRACT_BY_CODE_ID":
      return ResourceType.KV_WASM_CONTRACT_BY_CODE_ID;
    case 79:
    case "KV_WASM_PINNED_CODE_INDEX":
      return ResourceType.KV_WASM_PINNED_CODE_INDEX;
    case 80:
    case "KV_AUTH_GLOBAL_ACCOUNT_NUMBER":
      return ResourceType.KV_AUTH_GLOBAL_ACCOUNT_NUMBER;
    case 81:
    case "KV_AUTHZ":
      return ResourceType.KV_AUTHZ;
    case 82:
    case "KV_FEEGRANT":
      return ResourceType.KV_FEEGRANT;
    case 83:
    case "KV_FEEGRANT_ALLOWANCE":
      return ResourceType.KV_FEEGRANT_ALLOWANCE;
    case 84:
    case "KV_SLASHING":
      return ResourceType.KV_SLASHING;
    case 85:
    case "KV_SLASHING_VAL_SIGNING_INFO":
      return ResourceType.KV_SLASHING_VAL_SIGNING_INFO;
    case 86:
    case "KV_SLASHING_ADDR_PUBKEY_RELATION_KEY":
      return ResourceType.KV_SLASHING_ADDR_PUBKEY_RELATION_KEY;
    case 87:
    case "KV_DEX_MEM_ORDER":
      return ResourceType.KV_DEX_MEM_ORDER;
    case 88:
    case "KV_DEX_MEM_CANCEL":
      return ResourceType.KV_DEX_MEM_CANCEL;
    case 89:
    case "KV_DEX_MEM_DEPOSIT":
      return ResourceType.KV_DEX_MEM_DEPOSIT;
    case 90:
    case "KV_DEX_CONTRACT":
      return ResourceType.KV_DEX_CONTRACT;
    case 91:
    case "KV_DEX_LONG_ORDER_COUNT":
      return ResourceType.KV_DEX_LONG_ORDER_COUNT;
    case 92:
    case "KV_DEX_SHORT_ORDER_COUNT":
      return ResourceType.KV_DEX_SHORT_ORDER_COUNT;
    case 93:
    case "KV_BANK_DEFERRED":
      return ResourceType.KV_BANK_DEFERRED;
    case 95:
    case "KV_BANK_DEFERRED_MODULE_TX_INDEX":
      return ResourceType.KV_BANK_DEFERRED_MODULE_TX_INDEX;
    case 96:
    case "KV_EVM":
      return ResourceType.KV_EVM;
    case 97:
    case "KV_EVM_BALANCE":
      return ResourceType.KV_EVM_BALANCE;
    case 98:
    case "KV_EVM_TRANSIENT":
      return ResourceType.KV_EVM_TRANSIENT;
    case 99:
    case "KV_EVM_ACCOUNT_TRANSIENT":
      return ResourceType.KV_EVM_ACCOUNT_TRANSIENT;
    case 100:
    case "KV_EVM_MODULE_TRANSIENT":
      return ResourceType.KV_EVM_MODULE_TRANSIENT;
    case 101:
    case "KV_EVM_NONCE":
      return ResourceType.KV_EVM_NONCE;
    case 102:
    case "KV_EVM_RECEIPT":
      return ResourceType.KV_EVM_RECEIPT;
    case 103:
    case "KV_EVM_S2E":
      return ResourceType.KV_EVM_S2E;
    case 104:
    case "KV_EVM_E2S":
      return ResourceType.KV_EVM_E2S;
    case 105:
    case "KV_EVM_CODE_HASH":
      return ResourceType.KV_EVM_CODE_HASH;
    case 106:
    case "KV_EVM_CODE":
      return ResourceType.KV_EVM_CODE;
    case 107:
    case "KV_EVM_CODE_SIZE":
      return ResourceType.KV_EVM_CODE_SIZE;
    case 108:
    case "KV_BANK_WEI_BALANCE":
      return ResourceType.KV_BANK_WEI_BALANCE;
    case 109:
    case "KV_DEX_MEM_CONTRACTS_TO_PROCESS":
      return ResourceType.KV_DEX_MEM_CONTRACTS_TO_PROCESS;
    case 110:
    case "KV_DEX_MEM_DOWNSTREAM_CONTRACTS":
      return ResourceType.KV_DEX_MEM_DOWNSTREAM_CONTRACTS;
    case -1:
    case "UNRECOGNIZED":
    default:
      return ResourceType.UNRECOGNIZED;
  }
}

export function resourceTypeToJSON(object: ResourceType): string {
  switch (object) {
    case ResourceType.ANY:
      return "ANY";
    case ResourceType.KV:
      return "KV";
    case ResourceType.Mem:
      return "Mem";
    case ResourceType.DexMem:
      return "DexMem";
    case ResourceType.KV_BANK:
      return "KV_BANK";
    case ResourceType.KV_STAKING:
      return "KV_STAKING";
    case ResourceType.KV_WASM:
      return "KV_WASM";
    case ResourceType.KV_ORACLE:
      return "KV_ORACLE";
    case ResourceType.KV_DEX:
      return "KV_DEX";
    case ResourceType.KV_EPOCH:
      return "KV_EPOCH";
    case ResourceType.KV_TOKENFACTORY:
      return "KV_TOKENFACTORY";
    case ResourceType.KV_ORACLE_VOTE_TARGETS:
      return "KV_ORACLE_VOTE_TARGETS";
    case ResourceType.KV_ORACLE_AGGREGATE_VOTES:
      return "KV_ORACLE_AGGREGATE_VOTES";
    case ResourceType.KV_ORACLE_FEEDERS:
      return "KV_ORACLE_FEEDERS";
    case ResourceType.KV_STAKING_DELEGATION:
      return "KV_STAKING_DELEGATION";
    case ResourceType.KV_STAKING_VALIDATOR:
      return "KV_STAKING_VALIDATOR";
    case ResourceType.KV_AUTH:
      return "KV_AUTH";
    case ResourceType.KV_AUTH_ADDRESS_STORE:
      return "KV_AUTH_ADDRESS_STORE";
    case ResourceType.KV_BANK_SUPPLY:
      return "KV_BANK_SUPPLY";
    case ResourceType.KV_BANK_DENOM:
      return "KV_BANK_DENOM";
    case ResourceType.KV_BANK_BALANCES:
      return "KV_BANK_BALANCES";
    case ResourceType.KV_TOKENFACTORY_DENOM:
      return "KV_TOKENFACTORY_DENOM";
    case ResourceType.KV_TOKENFACTORY_METADATA:
      return "KV_TOKENFACTORY_METADATA";
    case ResourceType.KV_TOKENFACTORY_ADMIN:
      return "KV_TOKENFACTORY_ADMIN";
    case ResourceType.KV_TOKENFACTORY_CREATOR:
      return "KV_TOKENFACTORY_CREATOR";
    case ResourceType.KV_ORACLE_EXCHANGE_RATE:
      return "KV_ORACLE_EXCHANGE_RATE";
    case ResourceType.KV_ORACLE_VOTE_PENALTY_COUNTER:
      return "KV_ORACLE_VOTE_PENALTY_COUNTER";
    case ResourceType.KV_ORACLE_PRICE_SNAPSHOT:
      return "KV_ORACLE_PRICE_SNAPSHOT";
    case ResourceType.KV_STAKING_VALIDATION_POWER:
      return "KV_STAKING_VALIDATION_POWER";
    case ResourceType.KV_STAKING_TOTAL_POWER:
      return "KV_STAKING_TOTAL_POWER";
    case ResourceType.KV_STAKING_VALIDATORS_CON_ADDR:
      return "KV_STAKING_VALIDATORS_CON_ADDR";
    case ResourceType.KV_STAKING_UNBONDING_DELEGATION:
      return "KV_STAKING_UNBONDING_DELEGATION";
    case ResourceType.KV_STAKING_UNBONDING_DELEGATION_VAL:
      return "KV_STAKING_UNBONDING_DELEGATION_VAL";
    case ResourceType.KV_STAKING_REDELEGATION:
      return "KV_STAKING_REDELEGATION";
    case ResourceType.KV_STAKING_REDELEGATION_VAL_SRC:
      return "KV_STAKING_REDELEGATION_VAL_SRC";
    case ResourceType.KV_STAKING_REDELEGATION_VAL_DST:
      return "KV_STAKING_REDELEGATION_VAL_DST";
    case ResourceType.KV_STAKING_REDELEGATION_QUEUE:
      return "KV_STAKING_REDELEGATION_QUEUE";
    case ResourceType.KV_STAKING_VALIDATOR_QUEUE:
      return "KV_STAKING_VALIDATOR_QUEUE";
    case ResourceType.KV_STAKING_HISTORICAL_INFO:
      return "KV_STAKING_HISTORICAL_INFO";
    case ResourceType.KV_STAKING_UNBONDING:
      return "KV_STAKING_UNBONDING";
    case ResourceType.KV_STAKING_VALIDATORS_BY_POWER:
      return "KV_STAKING_VALIDATORS_BY_POWER";
    case ResourceType.KV_DISTRIBUTION:
      return "KV_DISTRIBUTION";
    case ResourceType.KV_DISTRIBUTION_FEE_POOL:
      return "KV_DISTRIBUTION_FEE_POOL";
    case ResourceType.KV_DISTRIBUTION_PROPOSER_KEY:
      return "KV_DISTRIBUTION_PROPOSER_KEY";
    case ResourceType.KV_DISTRIBUTION_OUTSTANDING_REWARDS:
      return "KV_DISTRIBUTION_OUTSTANDING_REWARDS";
    case ResourceType.KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR:
      return "KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR";
    case ResourceType.KV_DISTRIBUTION_DELEGATOR_STARTING_INFO:
      return "KV_DISTRIBUTION_DELEGATOR_STARTING_INFO";
    case ResourceType.KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS:
      return "KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS";
    case ResourceType.KV_DISTRIBUTION_VAL_CURRENT_REWARDS:
      return "KV_DISTRIBUTION_VAL_CURRENT_REWARDS";
    case ResourceType.KV_DISTRIBUTION_VAL_ACCUM_COMMISSION:
      return "KV_DISTRIBUTION_VAL_ACCUM_COMMISSION";
    case ResourceType.KV_DISTRIBUTION_SLASH_EVENT:
      return "KV_DISTRIBUTION_SLASH_EVENT";
    case ResourceType.KV_DEX_CONTRACT_LONGBOOK:
      return "KV_DEX_CONTRACT_LONGBOOK";
    case ResourceType.KV_DEX_CONTRACT_SHORTBOOK:
      return "KV_DEX_CONTRACT_SHORTBOOK";
    case ResourceType.KV_DEX_SETTLEMENT:
      return "KV_DEX_SETTLEMENT";
    case ResourceType.KV_DEX_PAIR_PREFIX:
      return "KV_DEX_PAIR_PREFIX";
    case ResourceType.KV_DEX_TWAP:
      return "KV_DEX_TWAP";
    case ResourceType.KV_DEX_PRICE:
      return "KV_DEX_PRICE";
    case ResourceType.KV_DEX_SETTLEMENT_ENTRY:
      return "KV_DEX_SETTLEMENT_ENTRY";
    case ResourceType.KV_DEX_REGISTERED_PAIR:
      return "KV_DEX_REGISTERED_PAIR";
    case ResourceType.KV_DEX_ORDER:
      return "KV_DEX_ORDER";
    case ResourceType.KV_DEX_CANCEL:
      return "KV_DEX_CANCEL";
    case ResourceType.KV_DEX_ACCOUNT_ACTIVE_ORDERS:
      return "KV_DEX_ACCOUNT_ACTIVE_ORDERS";
    case ResourceType.KV_DEX_ASSET_LIST:
      return "KV_DEX_ASSET_LIST";
    case ResourceType.KV_DEX_NEXT_ORDER_ID:
      return "KV_DEX_NEXT_ORDER_ID";
    case ResourceType.KV_DEX_NEXT_SETTLEMENT_ID:
      return "KV_DEX_NEXT_SETTLEMENT_ID";
    case ResourceType.KV_DEX_MATCH_RESULT:
      return "KV_DEX_MATCH_RESULT";
    case ResourceType.KV_DEX_SETTLEMENT_ORDER_ID:
      return "KV_DEX_SETTLEMENT_ORDER_ID";
    case ResourceType.KV_DEX_ORDER_BOOK:
      return "KV_DEX_ORDER_BOOK";
    case ResourceType.KV_ACCESSCONTROL:
      return "KV_ACCESSCONTROL";
    case ResourceType.KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING:
      return "KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING";
    case ResourceType.KV_WASM_CODE:
      return "KV_WASM_CODE";
    case ResourceType.KV_WASM_CONTRACT_ADDRESS:
      return "KV_WASM_CONTRACT_ADDRESS";
    case ResourceType.KV_WASM_CONTRACT_STORE:
      return "KV_WASM_CONTRACT_STORE";
    case ResourceType.KV_WASM_SEQUENCE_KEY:
      return "KV_WASM_SEQUENCE_KEY";
    case ResourceType.KV_WASM_CONTRACT_CODE_HISTORY:
      return "KV_WASM_CONTRACT_CODE_HISTORY";
    case ResourceType.KV_WASM_CONTRACT_BY_CODE_ID:
      return "KV_WASM_CONTRACT_BY_CODE_ID";
    case ResourceType.KV_WASM_PINNED_CODE_INDEX:
      return "KV_WASM_PINNED_CODE_INDEX";
    case ResourceType.KV_AUTH_GLOBAL_ACCOUNT_NUMBER:
      return "KV_AUTH_GLOBAL_ACCOUNT_NUMBER";
    case ResourceType.KV_AUTHZ:
      return "KV_AUTHZ";
    case ResourceType.KV_FEEGRANT:
      return "KV_FEEGRANT";
    case ResourceType.KV_FEEGRANT_ALLOWANCE:
      return "KV_FEEGRANT_ALLOWANCE";
    case ResourceType.KV_SLASHING:
      return "KV_SLASHING";
    case ResourceType.KV_SLASHING_VAL_SIGNING_INFO:
      return "KV_SLASHING_VAL_SIGNING_INFO";
    case ResourceType.KV_SLASHING_ADDR_PUBKEY_RELATION_KEY:
      return "KV_SLASHING_ADDR_PUBKEY_RELATION_KEY";
    case ResourceType.KV_DEX_MEM_ORDER:
      return "KV_DEX_MEM_ORDER";
    case ResourceType.KV_DEX_MEM_CANCEL:
      return "KV_DEX_MEM_CANCEL";
    case ResourceType.KV_DEX_MEM_DEPOSIT:
      return "KV_DEX_MEM_DEPOSIT";
    case ResourceType.KV_DEX_CONTRACT:
      return "KV_DEX_CONTRACT";
    case ResourceType.KV_DEX_LONG_ORDER_COUNT:
      return "KV_DEX_LONG_ORDER_COUNT";
    case ResourceType.KV_DEX_SHORT_ORDER_COUNT:
      return "KV_DEX_SHORT_ORDER_COUNT";
    case ResourceType.KV_BANK_DEFERRED:
      return "KV_BANK_DEFERRED";
    case ResourceType.KV_BANK_DEFERRED_MODULE_TX_INDEX:
      return "KV_BANK_DEFERRED_MODULE_TX_INDEX";
    case ResourceType.KV_EVM:
      return "KV_EVM";
    case ResourceType.KV_EVM_BALANCE:
      return "KV_EVM_BALANCE";
    case ResourceType.KV_EVM_TRANSIENT:
      return "KV_EVM_TRANSIENT";
    case ResourceType.KV_EVM_ACCOUNT_TRANSIENT:
      return "KV_EVM_ACCOUNT_TRANSIENT";
    case ResourceType.KV_EVM_MODULE_TRANSIENT:
      return "KV_EVM_MODULE_TRANSIENT";
    case ResourceType.KV_EVM_NONCE:
      return "KV_EVM_NONCE";
    case ResourceType.KV_EVM_RECEIPT:
      return "KV_EVM_RECEIPT";
    case ResourceType.KV_EVM_S2E:
      return "KV_EVM_S2E";
    case ResourceType.KV_EVM_E2S:
      return "KV_EVM_E2S";
    case ResourceType.KV_EVM_CODE_HASH:
      return "KV_EVM_CODE_HASH";
    case ResourceType.KV_EVM_CODE:
      return "KV_EVM_CODE";
    case ResourceType.KV_EVM_CODE_SIZE:
      return "KV_EVM_CODE_SIZE";
    case ResourceType.KV_BANK_WEI_BALANCE:
      return "KV_BANK_WEI_BALANCE";
    case ResourceType.KV_DEX_MEM_CONTRACTS_TO_PROCESS:
      return "KV_DEX_MEM_CONTRACTS_TO_PROCESS";
    case ResourceType.KV_DEX_MEM_DOWNSTREAM_CONTRACTS:
      return "KV_DEX_MEM_DOWNSTREAM_CONTRACTS";
    default:
      return "UNKNOWN";
  }
}

export enum WasmMessageSubtype {
  QUERY = 0,
  EXECUTE = 1,
  UNRECOGNIZED = -1,
}

export function wasmMessageSubtypeFromJSON(object: any): WasmMessageSubtype {
  switch (object) {
    case 0:
    case "QUERY":
      return WasmMessageSubtype.QUERY;
    case 1:
    case "EXECUTE":
      return WasmMessageSubtype.EXECUTE;
    case -1:
    case "UNRECOGNIZED":
    default:
      return WasmMessageSubtype.UNRECOGNIZED;
  }
}

export function wasmMessageSubtypeToJSON(object: WasmMessageSubtype): string {
  switch (object) {
    case WasmMessageSubtype.QUERY:
      return "QUERY";
    case WasmMessageSubtype.EXECUTE:
      return "EXECUTE";
    default:
      return "UNKNOWN";
  }
}

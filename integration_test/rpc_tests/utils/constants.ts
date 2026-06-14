/**
 * Shared primitive constants for the RPC suite. Single source of truth so values like
 * the HD derivation path and the usei↔wei scale are never re-declared per spec.
 */

/** Sei keys use cosmos coin type 118; the matching EVM key derives from the same path. */
export const SEI_HD_PATH = "m/44'/118'/0'/0/0";

/** 10^12 wei == 1 usei. Sei rounds native balances to whole usei. */
export const WEI_PER_USEI = 10n ** 12n;
/** Alias kept for the balance/fee-reconciliation specs that read this as `USEI`. */
export const USEI = WEI_PER_USEI;

/** Sei's staking precompile address (used by precompile-call fixtures). */
export const STAKING_PRECOMPILE_ADDRESS = '0x0000000000000000000000000000000000001005';

/** The all-zero 20-byte address. */
export const ZERO_ADDRESS = '0x' + '0'.repeat(40);

/** The all-zero 32-byte hash (empty sha3Uncles / mixHash, zero block-hash sentinel). */
export const ZERO_HASH = '0x' + '00'.repeat(32);

/** The all-zero 8-byte block nonce. */
export const ZERO_NONCE = '0x' + '00'.repeat(8);

/** Default Sei EVM chain id on the local devnet. */
export const DEFAULT_EVM_CHAIN_ID = 713714;

export const DOCKER_NODE = 'sei-node-0';
export const SEID_ENV = 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin';


/** Shared primitive constants for the precompile suite — single source of truth. */

/** Sei keys use cosmos coin type 118; the matching EVM key derives from the same path. */
export const SEI_HD_PATH = "m/44'/118'/0'/0/0";

/** 10^12 wei == 1 usei. Sei rounds native balances to whole usei. */
export const WEI_PER_USEI = 10n ** 12n;

/** The all-zero 20-byte address. */
export const ZERO_ADDRESS = '0x' + '0'.repeat(40);

export const DOCKER_NODE = 'sei-node-0';
export const SEID_ENV = 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin';

/** Password for the in-container `admin`/test keyring (docker devnet only). */
export const DOCKER_KEY_PASSWORD = '12345678';

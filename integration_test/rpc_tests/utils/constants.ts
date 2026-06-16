/** Shared primitive constants for the RPC suite — single source of truth. */
import path from 'node:path';
import { calculateFee, GasPrice } from '@cosmjs/stargate';

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

/** Password for the in-container `admin`/test keyring (docker devnet only). */
export const DOCKER_KEY_PASSWORD = '12345678';

/** In-container EVM RPC the `seid` CLI targets when registering CW20 pointers. */
export const DOCKER_EVM_RPC = 'http://localhost:8545';

/**
 * cw20_base wasm bundled with the suite, instantiated as the dual-VM fixture token.
 *
 * Provenance: a verbatim copy of the repo's canonical `contracts/wasm/cw20_base.wasm`
 * (the same CW20 base artifact `integration_test/dapp_tests` uploads). It is pinned by the
 * SHA256 below, which the bootstrap verifies before upload, so a swapped or tampered binary
 * fails loudly in CI/review rather than silently changing fixture behavior. To update it,
 * re-copy from `contracts/wasm/cw20_base.wasm` and refresh this hash in the same commit.
 */
export const CW20_WASM_PATH = path.resolve(__dirname, '..', 'contracts', 'cw20_base.wasm');

/** Pinned SHA256 of CW20_WASM_PATH (matches contracts/wasm/cw20_base.wasm). */
export const CW20_WASM_SHA256 =
    '68c6a4bdbb3edfe61c1c08c84bbaa954ffcd25492fb6c60f3cd0107aa2ccc207';

/** Flat fee for the one-off CW20 store + instantiate (10M gas @ 3.5usei). */
export const WASM_FEE = calculateFee(10_000_000, GasPrice.fromString('3.5usei'));

/** Flat fee for a single CW20 execute, e.g. transfer (1.5M gas @ 3.5usei). */
export const EXEC_FEE = calculateFee(1_500_000, GasPrice.fromString('3.5usei'));


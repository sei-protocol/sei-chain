import { ethers } from 'ethers';
import { EvmAccount, deployContract } from './evmUtils';
import { uint256Word, addressWord } from './format';

/**
 * Helpers for the storage-introspection specs (eth_getStorageAt, eth_getProof).
 *
 * Storage layout of contracts/TestERC20.sol (declaration order):
 *   slot 0  string  name
 *   slot 1  string  symbol
 *   (decimals is `constant` — no slot)
 *   slot 2  uint256 totalSupply
 *   slot 3  address owner
 *   slot 4  mapping(address => uint256)                       balanceOf
 *   slot 5  mapping(address => mapping(address => uint256))   allowance
 */
export const SLOT_TOTAL_SUPPLY = 2;
export const SLOT_OWNER = 3;
export const SLOT_BALANCEOF = 4;
export const SLOT_ALLOWANCE = 5;

/** Canonical 32-byte storage word: lower-case, 0x-prefixed, exactly 64 nibbles. */
export const STORAGE_WORD = /^0x[0-9a-f]{64}$/;

/** keccak256(empty) — the codeHash an EOA must report under EIP-1186. */
export const EMPTY_CODE_HASH =
    '0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470';
/** Root of an empty Merkle-Patricia trie — the storageHash of an account with no storage. */
export const EMPTY_STORAGE_ROOT =
    '0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421';

/** Storage slot of `mapping[key]` declared at `baseSlot`: keccak256(pad(key) ++ pad(slot)). */
export function mappingSlot(key: string, baseSlot: number | bigint): string {
    return ethers.keccak256(
        ethers.concat([ethers.zeroPadValue(key, 32), ethers.zeroPadValue(ethers.toBeHex(baseSlot), 32)]),
    );
}

/** Storage slot of `mapping[k1][k2]` declared at `baseSlot` (e.g. ERC20 allowance). */
export function nestedMappingSlot(k1: string, k2: string, baseSlot: number | bigint): string {
    return ethers.keccak256(ethers.concat([ethers.zeroPadValue(k2, 32), mappingSlot(k1, baseSlot)]));
}

/**
 * The 32-byte storage words eth_getStorageAt returns. These are the canonical word
 * encoders from ./format, re-exported under storage-flavoured names so the storage
 * specs read naturally (`storageWord(supply)`, `addressWord(owner)`).
 */
export { uint256Word as storageWord, addressWord };

export interface StorageScene {
    /** Freshly deployed TestERC20 with deterministic, known storage. */
    erc20: string;
    /** Deployer == the value stored in the `owner` slot and the allowance owner. */
    owner: EvmAccount;
    holder: string;
    spender: string;

    totalSupply: bigint;
    holderBalance: bigint;
    allowanceAmount: bigint;

    deployBlock: number;
    /** The block just before the mint (mintBlock-1): contract exists, balances/supply still 0. */
    preSeedBlock: number;
    /** Block of the final seeding tx (mint + approve applied). */
    seededBlock: number;
}

/**
 * Deploy a dedicated TestERC20 and seed it with known values so storage reads have an
 * exact, predictable expectation:
 *   - owner slot               = `deployer`
 *   - totalSupply slot         = holderBalance
 *   - balanceOf[holder]        = holderBalance
 *   - allowance[owner][spender] = allowanceAmount
 */
export async function seedStorageToken(
    deployer: EvmAccount,
    holderBalance = 1_000n,
    allowanceAmount = 777n,
): Promise<StorageScene> {
    const holder = ethers.Wallet.createRandom().address;
    const spender = ethers.Wallet.createRandom().address;

    const { contract, address, receipt } = await deployContract(
        deployer,
        'TestERC20.sol',
        [deployer.address],
        'TestERC20',
    );

    const mint = await (await contract.mint(holder, holderBalance)).wait();
    const approve = await (await contract.approve(spender, allowanceAmount)).wait();

    // preSeedBlock must be a height where the contract exists but the mint has NOT yet applied.
    // Block tags expose end-of-block state, so a pre-broadcast getBlockNumber() snapshot is unsafe:
    // if the mint lands in that same block the balance is already seeded there. Derive it from the
    // mint's actual height instead — mintBlock-1 is >= deployBlock (the mint can't share the already
    // committed deploy block) and strictly before the mint, so the holder balance is provably zero.
    const preSeedBlock = mint!.blockNumber - 1;

    return {
        erc20: address,
        owner: deployer,
        holder,
        spender,
        totalSupply: holderBalance,
        holderBalance,
        allowanceAmount,
        deployBlock: receipt.blockNumber,
        preSeedBlock,
        seededBlock: approve!.blockNumber,
    };
}

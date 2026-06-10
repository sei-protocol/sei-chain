import util from 'node:util';
import { ethers } from 'ethers';
import { expect } from 'chai';
import { EvmAccount, abiOf, deployContract } from './evmUtils';
import { ADDRESS_LOWER, HASH32, HEX_DATA, HEX_QUANTITY } from './format';
import { DOCKER_NODE, SEID_ENV, STAKING_PRECOMPILE_ADDRESS } from './constants';

const exec = util.promisify(require('node:child_process').exec);

/**
 * Shared helpers for the log/filter RPC specs (eth_getLogs, eth_newFilter,
 * eth_getFilterLogs, eth_getFilterChanges).
 *
 * Every spec deploys its *own* TestERC20 and emits a small, fully-known set of
 * events against it, so address-scoped queries return an exact, predictable set
 * of logs that can't be polluted by other specs sharing the chain.
 */

export const ERC20_LOG_IFACE = new ethers.Interface(abiOf('TestERC20.sol', 'TestERC20'));
export const TRANSFER_TOPIC = ERC20_LOG_IFACE.getEvent('Transfer')!.topicHash;
export const APPROVAL_TOPIC = ERC20_LOG_IFACE.getEvent('Approval')!.topicHash;

/** Filter handles are opaque random hex, not minimally-encoded quantities. */
export const FILTER_ID = /^0x[0-9a-f]+$/;

/**
 * The canonical Ethereum log fields. Sei returns exactly these; a modern geth
 * additionally returns `blockTimestamp`, so parity is asserted as "both carry
 * every core field" rather than strict key-set equality.
 */
export const CORE_LOG_KEYS = [
    'address',
    'blockHash',
    'blockNumber',
    'data',
    'logIndex',
    'removed',
    'topics',
    'transactionHash',
    'transactionIndex',
] as const;

/** Left-pad a 20-byte address into the 32-byte word used to match an indexed topic. */
export function addressTopic(addr: string): string {
    return ethers.zeroPadValue(ethers.getAddress(addr), 32).toLowerCase();
}

export { STAKING_PRECOMPILE_ADDRESS };

/**
 * Sei's staking precompile emits real EVM logs (Delegate / Undelegate / …) under its
 * own precompile address, so eth_getLogs can index them like any contract's events.
 */
export const STAKING_IFACE = new ethers.Interface([
    'function delegate(string valAddress) payable returns (bool)',
    'event Delegate(address indexed delegator, string validator, uint256 amount)',
]);
export const DELEGATE_TOPIC = STAKING_IFACE.getEvent('Delegate')!.topicHash;

/** First bonded validator's `seivaloper…` operator address, via the in-container CLI. */
export async function firstBondedValidator(): Promise<string> {
    const { stdout } = await exec(
        `docker exec ${DOCKER_NODE} /bin/bash -c '${SEID_ENV} && seid q staking validators -o json'`,
    );
    const vals = JSON.parse(stdout).validators as { operator_address: string; status: string }[];
    const bonded = vals.find(v => v.status === 'BOND_STATUS_BONDED') ?? vals[0];
    if (!bonded) throw new Error('firstBondedValidator: no validators returned');
    return bonded.operator_address;
}

/** Delegate `amountWei` to `validator` through the staking precompile (emits Delegate). */
export async function delegateViaPrecompile(
    account: EvmAccount,
    validator: string,
    amountWei: bigint,
): Promise<ethers.TransactionReceipt> {
    const staking = new ethers.Contract(STAKING_PRECOMPILE_ADDRESS, STAKING_IFACE, account.wallet);
    const tx = await staking.delegate(validator, { value: amountWei, gasLimit: 1_000_000n });
    const receipt = await tx.wait();
    if (!receipt) throw new Error('delegateViaPrecompile: delegate tx did not confirm');
    return receipt;
}

export interface LogScene {
    /** Address of the freshly deployed, isolated ERC20 the events came from. */
    erc20: string;
    /** The account that deployed the token and emitted every event. */
    emitter: EvmAccount;
    /** Two transfer recipients. */
    alice: string;
    bob: string;

    /** Block of the (event-less) contract deployment. */
    deployBlock: number;
    /** Transfer(0x0 -> emitter) from the mint. */
    mintBlock: number;
    /** Transfer(emitter -> alice). */
    aliceBlock: number;
    /** Transfer(emitter -> bob). */
    bobBlock: number;
    /** Approval(emitter -> alice). */
    approveBlock: number;

    /** First / last block carrying one of this scene's events. */
    firstEventBlock: number;
    lastEventBlock: number;

    /** Exact event counts emitted against `erc20`. */
    transferCount: number; // mint + 2 transfers
    approvalCount: number;
    totalCount: number;
}

/**
 * Deploy a dedicated ERC20 and emit a fixed scene of four events:
 *   1. mint     -> Transfer(0x0      -> emitter)
 *   2. transfer -> Transfer(emitter  -> alice)
 *   3. transfer -> Transfer(emitter  -> bob)
 *   4. approve  -> Approval(emitter  -> alice)
 *
 * Each lands in its own block on Sei, giving callers exact block bounds and
 * counts to assert against.
 */
export async function emitLogScene(
    deployer: EvmAccount,
    alice: string,
    bob: string,
): Promise<LogScene> {
    const { address, receipt } = await deployContract(
        deployer,
        'TestERC20.sol',
        [deployer.address],
        'TestERC20',
    );
    const token = new ethers.Contract(address, ERC20_LOG_IFACE, deployer.wallet);

    const mint = await (await token.mint(deployer.address, ethers.parseEther('1000'))).wait();
    const toAlice = await (await token.transfer(alice, 10n)).wait();
    const toBob = await (await token.transfer(bob, 20n)).wait();
    const approve = await (await token.approve(alice, 5n)).wait();

    return {
        erc20: address,
        emitter: deployer,
        alice,
        bob,
        deployBlock: receipt.blockNumber,
        mintBlock: mint!.blockNumber,
        aliceBlock: toAlice!.blockNumber,
        bobBlock: toBob!.blockNumber,
        approveBlock: approve!.blockNumber,
        firstEventBlock: mint!.blockNumber,
        lastEventBlock: approve!.blockNumber,
        transferCount: 3,
        approvalCount: 1,
        totalCount: 4,
    };
}

/**
 * Deploy a fresh, event-less TestERC20 and return a contract handle so a spec can
 * emit events on its own schedule — used when a filter must be installed *before*
 * the events it should observe are produced (eth_getFilterChanges).
 */
export async function deployLogToken(
    deployer: EvmAccount,
): Promise<{ address: string; token: ethers.Contract }> {
    const { address } = await deployContract(
        deployer,
        'TestERC20.sol',
        [deployer.address],
        'TestERC20',
    );
    const token = new ethers.Contract(address, ERC20_LOG_IFACE, deployer.wallet);
    return { address, token };
}

/** Assert a single eth_getLogs entry carries the canonical schema with valid types. */
export function expectLogShape(log: any, ctx = 'log'): void {
    CORE_LOG_KEYS.forEach(k =>
        expect(log, `${ctx}: has ${k}`).to.have.property(k),
    );
    expect(log.address, `${ctx}.address`).to.match(ADDRESS_LOWER);
    expect(log.topics, `${ctx}.topics is an array`).to.be.an('array');
    expect(log.topics.length, `${ctx}.topics has 1-4 entries`).to.be.within(1, 4);
    log.topics.forEach((t: string, i: number) =>
        expect(t, `${ctx}.topics[${i}]`).to.match(HASH32),
    );
    expect(log.data, `${ctx}.data`).to.match(HEX_DATA);
    expect(log.blockNumber, `${ctx}.blockNumber`).to.match(HEX_QUANTITY);
    expect(log.transactionIndex, `${ctx}.transactionIndex`).to.match(HEX_QUANTITY);
    expect(log.logIndex, `${ctx}.logIndex`).to.match(HEX_QUANTITY);
    expect(log.blockHash, `${ctx}.blockHash`).to.match(HASH32);
    expect(log.transactionHash, `${ctx}.transactionHash`).to.match(HASH32);
    expect(log.removed, `${ctx}.removed`).to.be.a('boolean');
}

/** Sorted key set of a log, for schema comparison. */
export function logKeys(log: any): string[] {
    return Object.keys(log).sort();
}

/**
 * Poll eth_getFilterChanges, accumulating the delivered deltas until `want` logs have
 * arrived (or the timeout elapses). eth_getFilterChanges returns only what's new since
 * the last poll keyed by block ranges, and on Sei's continuously-producing chain the
 * log cursor can trail the tx receipt by a poll — so the freshly emitted log may land
 * on a subsequent call rather than the first. Draining keeps the "delivered exactly
 * once" semantics verifiable without depending on exact poll timing.
 */
export async function drainFilterChanges(
    provider: ethers.JsonRpcProvider,
    filterId: string,
    want: number,
    timeoutMs = 15_000,
): Promise<any[]> {
    const collected: any[] = [];
    const deadline = Date.now() + timeoutMs;
    while (collected.length < want && Date.now() < deadline) {
        const delta = await provider.send('eth_getFilterChanges', [filterId]);
        collected.push(...delta);
        if (collected.length < want) await new Promise(r => setTimeout(r, 300));
    }
    return collected;
}

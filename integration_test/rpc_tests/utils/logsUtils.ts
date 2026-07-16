import util from 'node:util';
import { ethers } from 'ethers';
import { expect } from 'chai';
import { EvmAccount, abiOf, deployContract } from './evmUtils';
import { ADDRESS_LOWER, HASH32, HEX_DATA, HEX_QUANTITY, OPAQUE_HEX_ID, addressWord } from './format';
import { DOCKER_NODE, SEID_ENV, STAKING_PRECOMPILE_ADDRESS } from './constants';

const exec = util.promisify(require('node:child_process').exec);

/**
 * Shared helpers for the log/filter RPC specs (eth_getLogs, eth_newFilter, eth_getFilterLogs,
 * eth_getFilterChanges). Each spec deploys its *own* TestERC20 and emits a small, fully-known
 * event set, so address-scoped queries stay exact and uncontaminated by other specs on the chain.
 */

export const ERC20_LOG_IFACE = new ethers.Interface(abiOf('TestERC20.sol', 'TestERC20'));
export const TRANSFER_TOPIC = ERC20_LOG_IFACE.getEvent('Transfer')!.topicHash;
export const APPROVAL_TOPIC = ERC20_LOG_IFACE.getEvent('Approval')!.topicHash;

/** Filter handles are opaque random hex, not minimally-encoded quantities. */
export const FILTER_ID = OPAQUE_HEX_ID;

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
export const addressTopic = addressWord;

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

/** The scene's blocks + token, the bound shared by every LOG_FILTER_MATRIX case. */
const sceneBounds = (scene: LogScene) => ({
    fromBlock: ethers.toQuantity(scene.firstEventBlock),
    toBlock: ethers.toQuantity(scene.lastEventBlock),
    address: scene.erc20,
});

export interface LogFilterCase {
    /** Reused as the it() name and the assertion context string. */
    title: string;
    /**
     * Build the eth_getLogs-style criteria and its expectations from an emitted scene. Every case is
     * bounded to the scene's blocks + token so topic-only filters can't match unrelated chain history
     * and bounded subscriptions replay a finite set.
     */
    build: (scene: LogScene) => {
        criteria: Record<string, unknown>;
        expectedCount: number;
        check?: (logs: any[]) => void;
    };
}

/**
 * The canonical address/topic filter matrix every log-filter surface must honour identically —
 * eth_getLogs, eth_newFilter, eth_getFilterChanges and the eth_subscribe `logs` stream all funnel
 * through the same GetLogsByFilters matching. Each spec parameterises over this single table and
 * pins results to its own eth_getLogs oracle, so a change in filter semantics is updated in one
 * place instead of across every spec. Counts derive from emitLogScene's fixed four-event scene:
 *   Transfer(0x0 -> emitter), Transfer(emitter -> alice), Transfer(emitter -> bob),
 *   Approval(emitter -> alice).
 */
export const LOG_FILTER_MATRIX: LogFilterCase[] = [
    {
        title: 'no topics matches every event',
        build: scene => ({ criteria: sceneBounds(scene), expectedCount: scene.totalCount }),
    },
    {
        title: '[] (empty topics) matches every event',
        build: scene => ({
            criteria: { ...sceneBounds(scene), topics: [] },
            expectedCount: scene.totalCount,
        }),
    },
    {
        title: '[A] matches topic0 (the three Transfers)',
        build: scene => ({
            criteria: { ...sceneBounds(scene), topics: [TRANSFER_TOPIC] },
            expectedCount: scene.transferCount,
            check: logs => logs.forEach(l => expect(l.topics[0]).to.equal(TRANSFER_TOPIC)),
        }),
    },
    {
        title: '[[A, B]] matches a topic0 OR-set (Transfer OR Approval)',
        build: scene => ({
            criteria: { ...sceneBounds(scene), topics: [[TRANSFER_TOPIC, APPROVAL_TOPIC]] },
            expectedCount: scene.totalCount,
            check: logs =>
                logs.forEach(l => expect([TRANSFER_TOPIC, APPROVAL_TOPIC]).to.include(l.topics[0])),
        }),
    },
    {
        title: '[A, B] matches an indexed positional topic (Transfers sent by the emitter)',
        build: scene => {
            const sender = addressTopic(scene.emitter.address);
            return {
                criteria: { ...sceneBounds(scene), topics: [TRANSFER_TOPIC, sender] },
                expectedCount: 2,
                check: logs => logs.forEach(l => expect(l.topics[1]).to.equal(sender)),
            };
        },
    },
    {
        title: '[null, B] matches pos1 regardless of pos0 (emitter as from/owner)',
        build: scene => {
            const sender = addressTopic(scene.emitter.address);
            return {
                criteria: { ...sceneBounds(scene), topics: [null, sender] },
                expectedCount: 3,
                check: logs => logs.forEach(l => expect(l.topics[1]).to.equal(sender)),
            };
        },
    },
    {
        title: '[A, null, X] honours a wildcard slot + recipient (only the alice transfer)',
        build: scene => {
            const alice = addressTopic(scene.alice);
            return {
                criteria: { ...sceneBounds(scene), topics: [TRANSFER_TOPIC, null, alice] },
                expectedCount: 1,
                check: logs => expect(logs[0].topics[2]).to.equal(alice),
            };
        },
    },
    {
        title: '[A, null, [X, Y]] matches (X OR Y) in an indexed slot (alice or bob)',
        build: scene => {
            const alice = addressTopic(scene.alice);
            const bob = addressTopic(scene.bob);
            return {
                criteria: { ...sceneBounds(scene), topics: [TRANSFER_TOPIC, null, [alice, bob]] },
                expectedCount: 2,
                check: logs =>
                    logs.forEach(l => {
                        expect(l.topics[0]).to.equal(TRANSFER_TOPIC);
                        expect([alice, bob]).to.include(l.topics[2]);
                    }),
            };
        },
    },
    {
        title: '[A, [X, Y]] matches a nested OR-set in an indexed slot (minted-from-zero OR emitter sends)',
        build: scene => ({
            criteria: {
                ...sceneBounds(scene),
                topics: [
                    TRANSFER_TOPIC,
                    [addressTopic(ethers.ZeroAddress), addressTopic(scene.emitter.address)],
                ],
            },
            expectedCount: scene.transferCount,
        }),
    },
    {
        title: 'an address array unions logs (a non-emitting co-address adds nothing)',
        build: scene => ({
            criteria: {
                ...sceneBounds(scene),
                address: [scene.erc20, ethers.Wallet.createRandom().address],
            },
            expectedCount: scene.totalCount,
            check: logs => logs.forEach(l => expect(l.address).to.equal(scene.erc20.toLowerCase())),
        }),
    },
    {
        title: 'an address array combined with a topic0 filter (the three Transfers)',
        build: scene => ({
            criteria: {
                ...sceneBounds(scene),
                address: [scene.erc20, ethers.Wallet.createRandom().address],
                topics: [TRANSFER_TOPIC],
            },
            expectedCount: scene.transferCount,
            check: logs => logs.forEach(l => expect(l.topics[0]).to.equal(TRANSFER_TOPIC)),
        }),
    },
];

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

/**
 * Cross-check a log against the canonical transaction receipt it belongs to: every block/tx
 * identity field must match the receipt, the log must be canonical (removed:false), and the
 * receipt must carry an identical log at the same block-global logIndex.
 */
export function assertLogMatchesReceipt(log: any, receipt: any, ctx = 'log'): void {
    expect(receipt, `${ctx}: receipt exists`).to.be.an('object');
    expect(log.removed, `${ctx}.removed is false (canonical chain)`).to.equal(false);
    expect(log.transactionHash, `${ctx}.transactionHash`).to.equal(receipt.transactionHash);
    expect(log.blockHash, `${ctx}.blockHash`).to.equal(receipt.blockHash);
    expect(BigInt(log.blockNumber), `${ctx}.blockNumber`).to.equal(BigInt(receipt.blockNumber));
    expect(BigInt(log.transactionIndex), `${ctx}.transactionIndex`).to.equal(
        BigInt(receipt.transactionIndex),
    );
    const twin = receipt.logs.find((l: any) => l.logIndex === log.logIndex);
    expect(twin, `${ctx}: receipt carries a log at logIndex ${log.logIndex}`).to.not.equal(
        undefined,
    );
    expect(twin.address, `${ctx}.address matches the receipt log`).to.equal(log.address);
    expect(twin.data, `${ctx}.data matches the receipt log`).to.equal(log.data);
    expect(twin.topics, `${ctx}.topics match the receipt log`).to.deep.equal(log.topics);
}

/** Sorted key set of a log, for schema comparison. */
export function logKeys(log: any): string[] {
    return Object.keys(log).sort();
}

/**
 * Poll eth_getFilterChanges, accumulating deltas until `want` logs arrive (or the timeout
 * elapses). On Sei's continuously-producing chain the log cursor can trail the tx receipt by a
 * poll, so a freshly emitted log may land on a later call; draining keeps the "delivered exactly
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

export async function assertFilterChangesMatchGetLogs(
    provider: ethers.JsonRpcProvider,
    criteria: object,
    ctx = 'filter',
): Promise<any[]> {
    const oracle = await provider.send('eth_getLogs', [criteria]);
    const id = await provider.send('eth_newFilter', [criteria]);
    try {
        const got =
            oracle.length === 0
                ? await provider.send('eth_getFilterChanges', [id])
                : await drainFilterChanges(provider, id, oracle.length);
        expect(got, `${ctx}: eth_getFilterChanges matches eth_getLogs`).to.deep.equal(oracle);
        return got;
    } finally {
        await provider.send('eth_uninstallFilter', [id]);
    }
}

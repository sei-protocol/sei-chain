import { ethers } from 'ethers';
import { expect } from 'chai';
import { EvmAccount, abiOf, bytecodeOf, selfAuthorize } from './evmUtils';
import { RuntimeState } from './testUtils';
import { HASH32, BLOOM256, NONCE8, HEX_QUANTITY, HEX_DATA, ADDRESS } from './format';
import { STAKING_PRECOMPILE_ADDRESS, USEI } from './constants';

// Re-exported here so the block/receipt specs can pull these from the tx domain module.
export { STAKING_PRECOMPILE_ADDRESS, USEI };

/**
 * Shared fixtures + assertions for the eth_getBlockByNumber / eth_getBlockByHash
 * parity specs.
 *
 * The core idea: Sei (a Cosmos chain) naturally packs many transactions into one
 * block, so we build a single "rich" block carrying every EVM transaction type
 * (legacy / access-list / EIP-1559 / set-code), a contract deployment, a contract
 * call, plain EOA transfers and a precompile call — each from a *distinct* funded
 * sender, so we can later verify each sender's gas + fee against the block. For the
 * geth reference we send a single transaction and assert the block/tx field schema
 * matches.
 */

export const EMPTY_UNCLES_HASH =
    '0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347';
export const ZERO_HASH = '0x' + '00'.repeat(32);

// Fields every Sei *and* geth block carries (the canonical pre-Cancun header plus
// London's baseFeePerGas). Asserted present on both chains.
export const CORE_BLOCK_FIELDS = [
    'baseFeePerGas',
    'difficulty',
    'extraData',
    'gasLimit',
    'gasUsed',
    'hash',
    'logsBloom',
    'miner',
    'mixHash',
    'nonce',
    'number',
    'parentHash',
    'receiptsRoot',
    'sha3Uncles',
    'size',
    'stateRoot',
    'timestamp',
    'transactions',
    'transactionsRoot',
    'uncles',
] as const;

// Documented divergences in the header field set. Sei may attach `totalDifficulty`
// on recent blocks (it is dropped for older ones), so it is an *allowed* extra
// rather than a required field.
export const SEI_ONLY_BLOCK_FIELDS = ['totalDifficulty'] as const;
export const GETH_ONLY_BLOCK_FIELDS = [
    'blobGasUsed',
    'excessBlobGas',
    'parentBeaconBlockRoot',
    'requestsHash',
    'withdrawals',
    'withdrawalsRoot',
] as const;

// Fields every full transaction object carries on both chains (EIP-1559 shape).
export const CORE_TX_FIELDS = [
    'accessList',
    'blockHash',
    'blockNumber',
    'chainId',
    'from',
    'gas',
    'gasPrice',
    'hash',
    'input',
    'maxFeePerGas',
    'maxPriorityFeePerGas',
    'nonce',
    'r',
    's',
    'to',
    'transactionIndex',
    'type',
    'v',
    'value',
    'yParity',
] as const;
export const GETH_ONLY_TX_FIELDS = ['blockTimestamp'] as const;

export type TxKind =
    | 'legacy'
    | 'accessList'
    | 'eip1559'
    | 'setCode'
    | 'deploy'
    | 'erc20'
    | 'precompile';

export interface SentTx {
    kind: TxKind;
    type: number;
    sender: string;
    // Recipient as broadcast. null for contract-creation transactions.
    to: string | null;
    // Calldata as broadcast ('0x' for pure transfers). Lets a test assert the
    // block echoes back the exact input bytes it was given.
    data: string;
    value: bigint;
    // The exact nonce we pinned, so the block's reported nonce can be checked.
    nonce: number;
    // The exact fee caps we signed with. legacy/access-list set gasPrice; the
    // EIP-1559 / set-code txs set maxFeePerGas + maxPriorityFeePerGas.
    gasPrice?: bigint;
    maxFeePerGas?: bigint;
    maxPriorityFeePerGas?: bigint;
    hash: string;
    receipt: ethers.TransactionReceipt;
}

// The exact access list signed into the access-list transaction, exported so the
// spec can assert the block echoes it back byte-for-byte.
export const ACCESS_LIST_FIXTURE = [
    { address: '0x' + '11'.repeat(20), storageKeys: ['0x' + '00'.repeat(32)] },
] as const;

export interface RichBlock {
    number: number;
    hash: string;
    txs: SentTx[];
}

/**
 * Fee caps for the rich-block txs, priced off the live base fee. `feeMultiplier` and
 * `tipGwei` escalate per retry so a batch that split (or stalled behind a rising base
 * fee on a congested chain) outbids its way into a single block on the next attempt,
 * rather than waiting the chain out.
 */
async function pricing(
    provider: ethers.JsonRpcProvider,
    feeMultiplier = 3n,
    tipGwei = 1n,
): Promise<{
    maxFeePerGas: bigint;
    maxPriorityFeePerGas: bigint;
    gasPrice: bigint;
}> {
    const head = await provider.getBlock('latest');
    const base = head?.baseFeePerGas ?? ethers.parseUnits('1', 'gwei');
    const tip = ethers.parseUnits(tipGwei.toString(), 'gwei');
    const maxFeePerGas = base * feeMultiplier + tip;
    return { maxFeePerGas, maxPriorityFeePerGas: tip, gasPrice: maxFeePerGas };
}

const TRANSFER_VALUE = ethers.parseEther('0.001');
const rand = (): string => ethers.Wallet.createRandom().address;

/**
 * Broadcast one transaction of every kind, each from its own signer, and wait for
 * them to land in a single block. Retries the whole batch if the chain happens to
 * split them across blocks — each retry re-prices with a higher fee multiplier and
 * tip so the batch outbids its way into one block on a congested chain instead of
 * waiting the chain out. `signers` must hold at least 7 funded accounts.
 */
export async function buildRichSeiBlock(
    provider: ethers.JsonRpcProvider,
    runtime: RuntimeState,
    signers: EvmAccount[],
    attempts = 6,
): Promise<RichBlock> {
    if (signers.length < 7) {
        throw new Error(`buildRichSeiBlock needs >= 7 signers, got ${signers.length}`);
    }
    const erc20Iface = new ethers.Interface(abiOf('TestERC20.sol', 'TestERC20'));
    const erc20Bytecode = bytecodeOf('TestERC20.sol', 'TestERC20');
    const validatorsData = new ethers.Interface([
        'function validators(string status, bytes pagination) returns (bytes,bytes)',
    ]).encodeFunctionData('validators', ['BOND_STATUS_BONDED', '0x']);

    let lastErr: unknown;
    for (let attempt = 0; attempt < attempts; attempt++) {
        // Escalate the fee each retry (3x/1gwei, 5x/2gwei, 7x/3gwei, …) so a batch that
        // split or stalled behind a rising base fee outbids into a single block.
        const p = await pricing(provider, BigInt(3 + attempt * 2), BigInt(1 + attempt));
        const [sLegacy, sAccess, s1559, sSetCode, sDeploy, sErc20, sPrecompile] = signers;

                const [nLegacy, nAccess, n1559, nSetCode, nDeploy, nErc20, nPrecompile] = await Promise.all(
            signers.map(s => s.nonce('pending')),
        );
        const auth = await selfAuthorize(sSetCode, runtime.contracts.simpleAccount7702);

        // Pin every recipient + calldata up front so the assertions can reconcile the
        // block against exactly what we broadcast (fresh random recipients start at a
        // zero balance, so they must end the block holding exactly `value`).
        const toLegacy = rand();
        const toAccess = rand();
        const to1559 = rand();
        const deployData = ethers.concat([erc20Bytecode, erc20Iface.encodeDeploy([sDeploy.address])]);
        const erc20Data = erc20Iface.encodeFunctionData('transfer', [rand(), 0n]);

        type Plan = {
            kind: TxKind;
            type: number;
            sender: string;
            to: string | null;
            data: string;
            value: bigint;
            nonce: number;
            gasPrice?: bigint;
            maxFeePerGas?: bigint;
            maxPriorityFeePerGas?: bigint;
            send: () => Promise<ethers.TransactionResponse>;
        };
        const plans: Plan[] = [
            {
                kind: 'legacy',
                type: 0,
                sender: sLegacy.address,
                to: toLegacy,
                data: '0x',
                value: TRANSFER_VALUE,
                nonce: nLegacy,
                gasPrice: p.gasPrice,
                send: () =>
                    sLegacy.wallet.sendTransaction({
                        to: toLegacy,
                        value: TRANSFER_VALUE,
                        type: 0,
                        gasPrice: p.gasPrice,
                        gasLimit: 21000n,
                        nonce: nLegacy,
                    }),
            },
            {
                kind: 'accessList',
                type: 1,
                sender: sAccess.address,
                to: toAccess,
                data: '0x',
                value: TRANSFER_VALUE,
                nonce: nAccess,
                gasPrice: p.gasPrice,
                send: () =>
                    sAccess.wallet.sendTransaction({
                        to: toAccess,
                        value: TRANSFER_VALUE,
                        type: 1,
                        gasPrice: p.gasPrice,
                        accessList: ACCESS_LIST_FIXTURE as any,
                        gasLimit: 30000n,
                        nonce: nAccess,
                    }),
            },
            {
                kind: 'eip1559',
                type: 2,
                sender: s1559.address,
                to: to1559,
                data: '0x',
                value: TRANSFER_VALUE,
                nonce: n1559,
                maxFeePerGas: p.maxFeePerGas,
                maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                send: () =>
                    s1559.wallet.sendTransaction({
                        to: to1559,
                        value: TRANSFER_VALUE,
                        type: 2,
                        maxFeePerGas: p.maxFeePerGas,
                        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                        gasLimit: 21000n,
                        nonce: n1559,
                    }),
            },
            {
                kind: 'setCode',
                type: 4,
                sender: sSetCode.address,
                to: sSetCode.address,
                data: '0x',
                value: 0n,
                nonce: nSetCode,
                maxFeePerGas: p.maxFeePerGas,
                maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                send: () =>
                    sSetCode.wallet.sendTransaction({
                        to: sSetCode.address,
                        data: '0x',
                        type: 4,
                        authorizationList: [auth],
                        maxFeePerGas: p.maxFeePerGas,
                        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                        gasLimit: 200000n,
                        nonce: nSetCode,
                    }),
            },
            {
                kind: 'deploy',
                type: 2,
                sender: sDeploy.address,
                to: null,
                data: deployData,
                value: 0n,
                nonce: nDeploy,
                maxFeePerGas: p.maxFeePerGas,
                maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                send: () =>
                    sDeploy.wallet.sendTransaction({
                        data: deployData,
                        type: 2,
                        maxFeePerGas: p.maxFeePerGas,
                        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                        gasLimit: 1_500_000n,
                        nonce: nDeploy,
                    }),
            },
            {
                kind: 'erc20',
                type: 2,
                sender: sErc20.address,
                to: runtime.contracts.erc20,
                data: erc20Data,
                value: 0n,
                nonce: nErc20,
                maxFeePerGas: p.maxFeePerGas,
                maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                send: () =>
                    sErc20.wallet.sendTransaction({
                        to: runtime.contracts.erc20,
                        data: erc20Data,
                        type: 2,
                        maxFeePerGas: p.maxFeePerGas,
                        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                        gasLimit: 120000n,
                        nonce: nErc20,
                    }),
            },
            {
                kind: 'precompile',
                type: 2,
                sender: sPrecompile.address,
                to: STAKING_PRECOMPILE_ADDRESS,
                data: validatorsData,
                value: 0n,
                nonce: nPrecompile,
                maxFeePerGas: p.maxFeePerGas,
                maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                send: () =>
                    sPrecompile.wallet.sendTransaction({
                        to: STAKING_PRECOMPILE_ADDRESS,
                        data: validatorsData,
                        type: 2,
                        maxFeePerGas: p.maxFeePerGas,
                        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
                        gasLimit: 2_000_000n,
                        nonce: nPrecompile,
                    }),
            },
        ];

        try {
            const responses = await Promise.all(plans.map(pl => pl.send()));
            const receipts = await Promise.all(responses.map(r => r.wait(1, 25_000)));
            const blockNumbers = receipts.map(r => r!.blockNumber);
            const uniqueBlocks = new Set(blockNumbers);
            const allOk = receipts.every(r => r && (r.status === 1 || r.status === 0));
            if (uniqueBlocks.size === 1 && allOk) {
                const blockNumber = blockNumbers[0];
                const block = await provider.getBlock(blockNumber);
                const txs: SentTx[] = plans.map((pl, i) => ({
                    kind: pl.kind,
                    type: pl.type,
                    sender: pl.sender,
                    to: pl.to,
                    data: pl.data,
                    value: pl.value,
                    nonce: pl.nonce,
                    gasPrice: pl.gasPrice,
                    maxFeePerGas: pl.maxFeePerGas,
                    maxPriorityFeePerGas: pl.maxPriorityFeePerGas,
                    hash: responses[i].hash,
                    receipt: receipts[i] as ethers.TransactionReceipt,
                }));
                return { number: blockNumber, hash: block!.hash!, txs };
            }
            lastErr = new Error(
                `txs split across blocks ${[...uniqueBlocks].join(',')} on attempt ${attempt + 1}`,
            );
        } catch (e) {
            lastErr = e;
        }
    }
    throw new Error(`buildRichSeiBlock: could not pack one block after ${attempts} attempts: ${lastErr}`);
}

/** Send a single EIP-1559 transfer and return it with the block it landed in. */
export async function sendSingleTx(
    provider: ethers.JsonRpcProvider,
    signer: EvmAccount,
): Promise<{ number: number; hash: string; tx: SentTx }> {
    const p = await pricing(provider);
    const value = TRANSFER_VALUE;
    const to = rand();
    const resp = await signer.wallet.sendTransaction({
        to,
        value,
        type: 2,
        maxFeePerGas: p.maxFeePerGas,
        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
        gasLimit: 21000n,
    });
    const receipt = (await resp.wait(1, 60_000))!;
    const block = await provider.getBlock(receipt.blockNumber);
    return {
        number: receipt.blockNumber,
        hash: block!.hash!,
        tx: {
            kind: 'eip1559',
            type: 2,
            sender: signer.address,
            to,
            data: '0x',
            value,
            nonce: resp.nonce,
            maxFeePerGas: p.maxFeePerGas,
            maxPriorityFeePerGas: p.maxPriorityFeePerGas,
            hash: resp.hash,
            receipt,
        },
    };
}

/**
 * Broadcast a transaction that reverts on execution (an ERC20 transfer larger than
 * the sender's balance) but is still *included* in a block with status 0.
 */
export async function sendRevertingTx(
    provider: ethers.JsonRpcProvider,
    signer: EvmAccount,
    erc20Address: string,
): Promise<SentTx> {
    const erc20Iface = new ethers.Interface(abiOf('TestERC20.sol', 'TestERC20'));
    const p = await pricing(provider);
    const data = erc20Iface.encodeFunctionData('transfer', [rand(), ethers.parseEther('1000000000')]);
    const resp = await signer.wallet.sendTransaction({
        to: erc20Address,
        data,
        type: 2,
        maxFeePerGas: p.maxFeePerGas,
        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
        // Explicit cap: the call reverts, so eth_estimateGas would throw.
        gasLimit: 120000n,
    });
    // wait() throws CALL_EXCEPTION on a status-0 receipt; waitForTransaction does not,
    // which is exactly what we want — the tx is mined, just reverted.
    const receipt = (await provider.waitForTransaction(resp.hash, 1, 60_000))!;
    return {
        kind: 'erc20',
        type: 2,
        sender: signer.address,
        to: erc20Address,
        data,
        value: 0n,
        nonce: resp.nonce,
        maxFeePerGas: p.maxFeePerGas,
        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
        hash: resp.hash,
        receipt,
    };
}

/**
 * Sign (but do not broadcast) a well-formed legacy transaction whose gas limit is
 * below the 21000 intrinsic floor. Submitting it must be *rejected* by the node with
 * an "intrinsic gas too low" error whose numbers (have/want) are chain-independent,
 * so Sei and geth produce byte-identical errors. Returns the raw payload + its hash.
 */
export async function signBelowIntrinsicTx(
    provider: ethers.JsonRpcProvider,
    signer: EvmAccount,
): Promise<{ raw: string; hash: string }> {
    const [net, nonce, head] = await Promise.all([
        provider.getNetwork(),
        signer.nonce('pending'),
        provider.getBlock('latest'),
    ]);
    const base = head?.baseFeePerGas ?? ethers.parseUnits('1', 'gwei');
    const raw = await signer.wallet.signTransaction({
        to: '0x' + '00'.repeat(19) + '01',
        value: 0n,
        gasLimit: 1000n, // below the 21000 intrinsic floor → rejected pre-execution
        gasPrice: base * 2n + ethers.parseUnits('1', 'gwei'),
        nonce,
        chainId: net.chainId,
        type: 0,
    });
    return { raw, hash: ethers.keccak256(raw) };
}

/** Assert every documented header field is present and canonically encoded. */
export function assertCanonicalHeader(block: any, opts: { hasTxs: boolean }): void {
    for (const f of CORE_BLOCK_FIELDS) {
        expect(block, `header is missing ${f}`).to.have.property(f);
    }
    expect(block.number, 'number').to.match(HEX_QUANTITY);
    expect(block.hash, 'hash').to.match(HASH32);
    expect(block.parentHash, 'parentHash').to.match(HASH32);
    expect(block.nonce, 'nonce').to.match(NONCE8);
    expect(block.sha3Uncles, 'sha3Uncles == empty-uncles').to.equal(EMPTY_UNCLES_HASH);
    expect(block.logsBloom, 'logsBloom is 256 bytes').to.match(BLOOM256);
    expect(block.transactionsRoot, 'transactionsRoot').to.match(HASH32);
    expect(block.stateRoot, 'stateRoot').to.match(HASH32);
    expect(block.receiptsRoot, 'receiptsRoot').to.match(HASH32);
    expect(block.mixHash, 'mixHash').to.match(HASH32);
    expect(block.miner, 'miner').to.match(ADDRESS);
    expect(block.difficulty, 'difficulty').to.match(HEX_QUANTITY);
    expect(block.extraData, 'extraData').to.match(HEX_DATA);
    expect(block.size, 'size').to.match(HEX_QUANTITY);
    expect(block.gasLimit, 'gasLimit').to.match(HEX_QUANTITY);
    expect(block.gasUsed, 'gasUsed').to.match(HEX_QUANTITY);
    expect(block.timestamp, 'timestamp').to.match(HEX_QUANTITY);
    expect(block.baseFeePerGas, 'baseFeePerGas').to.match(HEX_QUANTITY);
    expect(block.uncles, 'uncles is an array').to.be.an('array');
    expect(block.uncles.length, 'no uncles').to.equal(0);
    expect(block.transactions, 'transactions is an array').to.be.an('array');
    expect(BigInt(block.gasLimit) > 0n, 'gasLimit > 0').to.equal(true);
    expect(BigInt(block.gasUsed) <= BigInt(block.gasLimit), 'gasUsed <= gasLimit').to.equal(true);
    if (opts.hasTxs) {
        expect(block.transactionsRoot, 'non-empty block has a real txsRoot').to.not.equal(ZERO_HASH);
        expect(block.receiptsRoot, 'non-empty block has a real receiptsRoot').to.not.equal(ZERO_HASH);
        expect(BigInt(block.gasUsed) > 0n, 'non-empty block burned gas').to.equal(true);
    }
}

// Fields present on every transaction object regardless of type (legacy type-0 has
// no accessList / maxFeePerGas, so those live in the type-2 CORE_TX_FIELDS set used
// only by the geth parity comparison).
const UNIVERSAL_TX_FIELDS = [
    'blockHash',
    'blockNumber',
    'from',
    'gas',
    'gasPrice',
    'hash',
    'input',
    'nonce',
    'r',
    's',
    'to',
    'transactionIndex',
    'type',
    'v',
    'value',
] as const;

/** Assert a full transaction object is canonically encoded and linked to its block. */
export function assertCanonicalTx(tx: any, block: any): void {
    for (const f of UNIVERSAL_TX_FIELDS) {
        expect(tx, `tx is missing ${f}`).to.have.property(f);
    }
    expect(tx.hash, 'tx.hash').to.match(HASH32);
    expect(tx.blockHash, 'tx.blockHash == block.hash').to.equal(block.hash);
    expect(BigInt(tx.blockNumber), 'tx.blockNumber == block.number').to.equal(BigInt(block.number));
    expect(tx.transactionIndex, 'transactionIndex').to.match(HEX_QUANTITY);
    expect(tx.from, 'from').to.match(ADDRESS);
    expect(tx.to === null || ADDRESS.test(tx.to), 'to is an address or null (creation)').to.equal(true);
    expect(tx.value, 'value').to.match(HEX_QUANTITY);
    expect(tx.gas, 'gas').to.match(HEX_QUANTITY);
    expect(tx.gasPrice, 'gasPrice').to.match(HEX_QUANTITY);
    expect(tx.nonce, 'nonce').to.match(HEX_QUANTITY);
    expect(tx.type, 'type').to.match(HEX_QUANTITY);
    expect(tx.input, 'input').to.match(HEX_DATA);
}

/**
 * Verify the block's gas accounting: the block's gasUsed equals the sum of every
 * listed transaction's receipt gasUsed, that per-receipt gasUsed is positive, that
 * cumulativeGasUsed rises monotonically with transaction index, and that the final
 * cumulativeGasUsed equals the block's gasUsed. Robust on both chains (pure gas units).
 */
export async function assertGasAccounting(
    provider: ethers.JsonRpcProvider,
    block: any,
): Promise<void> {
    const hashes: string[] = (block.transactions as any[]).map(t =>
        typeof t === 'string' ? t : t.hash,
    );
    const receipts = await Promise.all(hashes.map(h => provider.getTransactionReceipt(h)));
    const summed = receipts.reduce((acc, r) => acc + r!.gasUsed, 0n);
    expect(summed, 'block.gasUsed == Σ receipt.gasUsed').to.equal(BigInt(block.gasUsed));

    // cumulativeGasUsed must be strictly increasing in tx index and end at gasUsed.
    const ordered = [...receipts].sort((a, b) => a!.index - b!.index);
    let prev = 0n;
    let running = 0n;
    for (const r of ordered) {
        expect(r!.gasUsed > 0n, `receipt ${r!.index} burned gas`).to.equal(true);
        running += r!.gasUsed;
        expect(
            r!.cumulativeGasUsed === running,
            `cumulativeGasUsed[${r!.index}] == running Σ gasUsed`,
        ).to.equal(true);
        expect(r!.cumulativeGasUsed > prev, `cumulativeGasUsed strictly increasing`).to.equal(true);
        prev = r!.cumulativeGasUsed;
    }
    if (ordered.length > 0) {
        expect(
            ordered[ordered.length - 1]!.cumulativeGasUsed,
            'final cumulativeGasUsed == block.gasUsed',
        ).to.equal(BigInt(block.gasUsed));
    }
}

const BASE_TX_GAS = 21_000n;
const ACCESS_LIST_ADDRESS_GAS = 2_400n;
const ACCESS_LIST_STORAGE_KEY_GAS = 1_900n;

/**
 * Exact intrinsic gas a *pure value transfer* (empty calldata) must burn: the 21000
 * base cost plus EIP-2930 access-list pricing. A transfer does no EVM execution, so
 * the receipt's gasUsed must equal this number to the gas — a far stronger check than
 * "gasUsed <= gasLimit". `txInBlock` is the full transaction object from the block.
 */
export function expectedTransferGas(txInBlock: any): bigint {
    let gas = BASE_TX_GAS;
    const al = txInBlock.accessList;
    if (Array.isArray(al)) {
        for (const entry of al) {
            gas += ACCESS_LIST_ADDRESS_GAS;
            gas += BigInt(entry.storageKeys?.length ?? 0) * ACCESS_LIST_STORAGE_KEY_GAS;
        }
    }
    return gas;
}

/** Rebuild a signed ethers Transaction from a block's full transaction object. */
function reconstructTx(tx: any): ethers.Transaction {
    const type = Number(tx.type);
    const yParity =
        tx.yParity !== undefined && tx.yParity !== null
            ? Number(tx.yParity)
            : // legacy EIP-155: 35 is odd, +2*chainId is even, so v parity flips yParity.
              Number(tx.v) % 2 === 1
              ? 0
              : 1;
    return ethers.Transaction.from({
        type,
        chainId: tx.chainId !== undefined ? BigInt(tx.chainId) : undefined,
        nonce: Number(tx.nonce),
        gasLimit: BigInt(tx.gas),
        gasPrice: type === 0 || type === 1 ? BigInt(tx.gasPrice) : undefined,
        maxFeePerGas: type >= 2 ? BigInt(tx.maxFeePerGas) : undefined,
        maxPriorityFeePerGas: type >= 2 ? BigInt(tx.maxPriorityFeePerGas) : undefined,
        to: tx.to,
        value: BigInt(tx.value),
        data: tx.input,
        accessList: tx.accessList ?? undefined,
        signature: ethers.Signature.from({ r: tx.r, s: tx.s, yParity: (yParity === 1 ? 1 : 0) as 0 | 1 }),
    } as ethers.TransactionLike);
}

/**
 * Verify the *actual bytes* the block reports. For every transaction whose type we
 * can re-encode (legacy / access-list / EIP-1559), rebuild it from the block's
 * reported fields, RLP-serialize it, and assert keccak256(bytes) == the reported
 * hash — proving the fields encode byte-for-byte to the real signed transaction.
 * Then assert block.size (the RLP byte length of the whole block) strictly exceeds
 * the summed transaction payload bytes, since the header + RLP framing add more.
 * Returns how many transactions were byte-verified.
 */
export function assertActualBytesAndSize(block: any): { verified: number; txBytes: number } {
    let txBytes = 0;
    let verified = 0;
    for (const tx of block.transactions as any[]) {
        if (typeof tx === 'string') continue;
        // Type-4 (set-code) authorization lists are not round-tripped by ethers here;
        // skip them for the byte check (the size lower bound stays valid without them).
        if (Number(tx.type) === 4) continue;
        let rebuilt: ethers.Transaction | null = null;
        try {
            rebuilt = reconstructTx(tx);
        } catch {
            rebuilt = null;
        }
        if (rebuilt && rebuilt.hash === tx.hash) {
            verified++;
            txBytes += ethers.dataLength(rebuilt.serialized);
        }
    }
    const sizeBytes = Number(BigInt(block.size));
    expect(sizeBytes > 0, 'block.size is positive').to.equal(true);
    expect(
        sizeBytes > txBytes,
        `block.size (${sizeBytes}) exceeds summed tx payload bytes (${txBytes})`,
    ).to.equal(true);
    return { verified, txBytes };
}

/**
 * Assert the block echoes back, exactly, the values we signed each transaction with:
 * the sender, the pinned nonce, the chain id, and the fee caps. These are all inputs
 * we control at send time, so the block must reflect them to the wei / unit.
 */
export function assertReportedSendFields(tx: any, sent: SentTx, chainId: bigint): void {
    expect(tx.from.toLowerCase(), `${sent.kind} from == the signer`).to.equal(
        sent.sender.toLowerCase(),
    );
    expect(BigInt(tx.nonce), `${sent.kind} nonce == the nonce we pinned`).to.equal(BigInt(sent.nonce));
    if (tx.chainId !== undefined && tx.chainId !== null) {
        expect(BigInt(tx.chainId), `${sent.kind} chainId`).to.equal(chainId);
    }
    if (sent.maxFeePerGas !== undefined) {
        expect(BigInt(tx.maxFeePerGas), `${sent.kind} maxFeePerGas`).to.equal(sent.maxFeePerGas);
        expect(BigInt(tx.maxPriorityFeePerGas), `${sent.kind} maxPriorityFeePerGas`).to.equal(
            sent.maxPriorityFeePerGas!,
        );
    }
    // Legacy / access-list transactions echo the signed gasPrice verbatim.
    if (sent.gasPrice !== undefined && (sent.type === 0 || sent.type === 1)) {
        expect(BigInt(tx.gasPrice), `${sent.kind} gasPrice`).to.equal(sent.gasPrice);
    }
}

// Set the three Bloom bits for one entry (an address or a topic), per the yellow
// paper's M3:2048 scheme as implemented by go-ethereum's bloom9.
function bloomAdd(bloom: Uint8Array, data: string): void {
    const h = ethers.getBytes(ethers.keccak256(data));
    for (const i of [0, 2, 4]) {
        const bit = ((h[i] << 8) | h[i + 1]) & 0x7ff;
        bloom[256 - 1 - (bit >> 3)] |= 1 << (bit & 7);
    }
}

/** Recompute a block's logsBloom from the logs its receipts emitted. */
export function computeLogsBloom(receipts: ethers.TransactionReceipt[]): string {
    const bloom = new Uint8Array(256);
    for (const r of receipts) {
        for (const log of r.logs) {
            bloomAdd(bloom, log.address);
            for (const topic of log.topics) bloomAdd(bloom, topic);
        }
    }
    return ethers.hexlify(bloom);
}

/**
 * Verify the header's logsBloom is exactly the Bloom filter of every log emitted by
 * the block's transactions — i.e. the events our txs produced (e.g. the ERC20
 * Transfer) are reflected in the canonical bloom, bit for bit.
 */
export async function assertLogsBloom(
    provider: ethers.JsonRpcProvider,
    block: any,
): Promise<void> {
    const hashes: string[] = (block.transactions as any[]).map(t =>
        typeof t === 'string' ? t : t.hash,
    );
    const receipts = await Promise.all(hashes.map(h => provider.getTransactionReceipt(h)));
    const expected = computeLogsBloom(receipts.filter((r): r is ethers.TransactionReceipt => !!r));
    expect(block.logsBloom, 'logsBloom == Bloom(all emitted logs)').to.equal(expected);
}

/**
 * Deliberately raise the base fee: fire repeated heavy gas-burner transactions from
 * every signer until the chain has produced blocks above its gas target. Returns the
 * pre-burst base fee and the block range the burst landed in so a test can verify the
 * baseFeePerGas the block reports afterwards. Mirrors eth_feeHistory's burnBurst.
 */
export async function burnGasBurst(
    provider: ethers.JsonRpcProvider,
    runtime: RuntimeState,
    signers: EvmAccount[],
    rounds = 12,
): Promise<{ beforeBaseFee: bigint; minBlock: number; maxBlock: number }> {
    const burnerIface = new ethers.Interface(abiOf('GasBurner.sol', 'RealGasBurner'));
    const burner = runtime.contracts.gasBurner;
    const GAS_LIMIT = 6_000_000n;
    const ITERATIONS = 200n;
    const tip = ethers.parseUnits('2', 'gwei');
    const baseFee = async (): Promise<bigint> =>
        BigInt((await provider.send('eth_getBlockByNumber', ['latest', false])).baseFeePerGas ?? '0x0');

    const beforeBaseFee = await baseFee();
    let minBlock = Number.MAX_SAFE_INTEGER;
    let maxBlock = 0;
    for (let round = 0; round < rounds; round++) {
        const base = await baseFee();
        const maxFee = base * 4n + tip;
        const sends: Promise<void>[] = [];
        for (let i = 0; i < signers.length; i++) {
            const s = signers[i];
            if ((await s.balance()) < GAS_LIMIT * maxFee) continue;
            const data = burnerIface.encodeFunctionData('burnGasIterations', [
                BigInt(round * 100 + i),
                ITERATIONS,
            ]);
            sends.push(
                s.wallet
                    .sendTransaction({
                        to: burner,
                        data,
                        gasLimit: GAS_LIMIT,
                        maxFeePerGas: maxFee,
                        maxPriorityFeePerGas: tip,
                        type: 2,
                    })
                    .then(t => t.wait())
                    .then(r => {
                        if (r) {
                            minBlock = Math.min(minBlock, r.blockNumber);
                            maxBlock = Math.max(maxBlock, r.blockNumber);
                        }
                    })
                    .catch(() => undefined),
            );
        }
        if (sends.length === 0) break;
        await Promise.all(sends);
    }
    return { beforeBaseFee, minBlock, maxBlock };
}

// ===========================================================================
// Block receipts (eth_getBlockReceipts) shape + reconciliation helpers.
// ===========================================================================

// The receipt field set returned by both Sei and geth (verified live: byte-identical to
// eth_getTransactionReceipt, and the same keys on both chains — except `to`, which Sei
// omits on a creation receipt while geth returns `to: null`).
export const CORE_RECEIPT_FIELDS = [
    'blockHash',
    'blockNumber',
    'contractAddress',
    'cumulativeGasUsed',
    'effectiveGasPrice',
    'from',
    'gasUsed',
    'logs',
    'logsBloom',
    'status',
    'to',
    'transactionHash',
    'transactionIndex',
    'type',
] as const;

// A transaction object (eth_getTransactionBy*) describes the *signed intent*; a receipt
// (eth_getBlockReceipts / eth_getTransactionReceipt) describes the *execution outcome*.
// The two are deliberately disjoint apart from the block-position / identity fields below
// — plus the pairing tx.gasPrice ⇔ receipt.effectiveGasPrice (the realised gas price) and
// tx.hash ⇔ receipt.transactionHash (the same value under different key names).
export const TX_RECEIPT_SHARED_FIELDS = [
    'blockHash',
    'blockNumber',
    'from',
    'to',
    'transactionIndex',
    'type',
] as const;

/** A block specifier accepted by the block/count endpoints: a height, a tag, or a block hash. */
export type BlockSpec = number | string;

/** eth_getBlockReceipts wrapper that hex-encodes a numeric height for you. */
export function blockReceipts(provider: ethers.JsonRpcProvider, spec: BlockSpec): Promise<any[]> {
    const param = typeof spec === 'number' ? ethers.toQuantity(spec) : spec;
    return provider.send('eth_getBlockReceipts', [param]);
}

/** Every receipt field is present, canonically encoded and linked to its block + index. */
export function assertCanonicalReceipt(
    rc: any,
    blockHash: string,
    blockNumber: number,
    index: number,
): void {
    // `to` is the one field that diverges: Sei omits it on a creation receipt while geth
    // returns `to: null`. Every other field is present on both chains for every receipt.
    for (const f of CORE_RECEIPT_FIELDS) {
        if (f === 'to') continue;
        expect(rc, `receipt missing ${f}`).to.have.property(f);
    }
    expect(rc.transactionHash, 'transactionHash').to.match(HASH32);
    expect(rc.blockHash, 'blockHash == block.hash').to.equal(blockHash);
    expect(BigInt(rc.blockNumber), 'blockNumber == block.number').to.equal(BigInt(blockNumber));
    expect(BigInt(rc.transactionIndex), 'transactionIndex is sequential').to.equal(BigInt(index));
    expect(rc.from, 'from').to.match(ADDRESS);
    expect(rc.cumulativeGasUsed, 'cumulativeGasUsed').to.match(HEX_QUANTITY);
    expect(rc.gasUsed, 'gasUsed').to.match(HEX_QUANTITY);
    expect(rc.effectiveGasPrice, 'effectiveGasPrice').to.match(HEX_QUANTITY);
    expect(rc.logsBloom, 'logsBloom is 256 bytes').to.match(BLOOM256);
    expect(rc.type, 'type').to.match(HEX_QUANTITY);
    expect(['0x0', '0x1'], 'status is 0x0 or 0x1').to.include(rc.status);
    expect(rc.logs, 'logs is an array').to.be.an('array');
    expect(BigInt(rc.gasUsed) > 0n, 'gasUsed > 0').to.equal(true);
    // contractAddress is populated iff the transaction was a contract creation.
    const isCreation = rc.contractAddress !== null && rc.contractAddress !== undefined;
    if (isCreation) {
        expect(rc.contractAddress, 'creation sets contractAddress').to.match(ADDRESS);
        // `to` is either absent (Sei) or null (geth) — never an address.
        expect(rc.to ?? null, 'creation receipt has no recipient').to.equal(null);
    } else {
        expect(rc, 'non-creation receipt has to').to.have.property('to');
        expect(rc.to, 'to is an address').to.match(ADDRESS);
        expect(rc.contractAddress, 'non-creation has a null contractAddress').to.equal(null);
    }
}

/** The effective gas price a receipt must report given the block's base fee. */
export function expectedEffectiveGasPrice(sent: SentTx, baseFee: bigint): bigint {
    // Legacy / access-list transactions pay exactly their signed gas price.
    if (sent.type === 0 || sent.type === 1) return sent.gasPrice!;
    // EIP-1559 / set-code: base fee + min(priority cap, maxFee - base).
    const room = sent.maxFeePerGas! - baseFee;
    const tip = sent.maxPriorityFeePerGas! < room ? sent.maxPriorityFeePerGas! : room;
    return baseFee + tip;
}

// ===========================================================================
// Raw transaction endpoints (eth_getRawTransactionBy*) — geth-only.
// ===========================================================================

// The raw-transaction endpoints return the RLP-encoded *signed* transaction. Sei does not
// implement them (it answers -32601), so these are primarily used to verify geth's output
// and to document the divergence; see eth_getBlockReceipts.spec.ts.
export const RAW_TX_BY_HASH = 'eth_getRawTransactionByHash';
export const RAW_TX_BY_BLOCK_HASH_AND_INDEX = 'eth_getRawTransactionByBlockHashAndIndex';
export const RAW_TX_BY_BLOCK_NUMBER_AND_INDEX = 'eth_getRawTransactionByBlockNumberAndIndex';

export const RAW_TX_METHODS = [
    RAW_TX_BY_HASH,
    RAW_TX_BY_BLOCK_HASH_AND_INDEX,
    RAW_TX_BY_BLOCK_NUMBER_AND_INDEX,
] as const;

/**
 * Decode a raw signed transaction and assert it re-derives every signed field reported by
 * the JSON-RPC transaction object (eth_getTransactionByHash). Proves the raw bytes are the
 * authentic, self-consistent encoding: keccak256(raw) is the hash, and the recovered
 * sender / nonce / value / gas / fees / signature all match. Returns the decoded tx.
 */
export function assertRawTxMatches(raw: string, txObject: any): ethers.Transaction {
    const decoded = ethers.Transaction.from(raw);
    expect(ethers.keccak256(raw), 'keccak256(raw) == tx hash').to.equal(txObject.hash);
    expect(decoded.hash, 'decoded hash == tx hash').to.equal(txObject.hash);
    expect(decoded.from?.toLowerCase(), 'recovered sender == from').to.equal(
        txObject.from.toLowerCase(),
    );
    expect(BigInt(decoded.nonce), 'nonce').to.equal(BigInt(txObject.nonce));
    expect((decoded.to ?? null)?.toLowerCase() ?? null, 'to').to.equal(
        (txObject.to ?? null)?.toLowerCase() ?? null,
    );
    expect(decoded.value, 'value').to.equal(BigInt(txObject.value));
    expect(decoded.gasLimit, 'gas limit').to.equal(BigInt(txObject.gas));
    expect(BigInt(decoded.type ?? 0), 'type').to.equal(BigInt(txObject.type));
    if (txObject.maxFeePerGas !== undefined) {
        expect(decoded.maxFeePerGas, 'maxFeePerGas').to.equal(BigInt(txObject.maxFeePerGas));
        expect(decoded.maxPriorityFeePerGas, 'maxPriorityFeePerGas').to.equal(
            BigInt(txObject.maxPriorityFeePerGas),
        );
    }
    if (txObject.gasPrice !== undefined && (decoded.type === 0 || decoded.type === 1)) {
        expect(decoded.gasPrice, 'gasPrice').to.equal(BigInt(txObject.gasPrice));
    }
    // Compare signature scalars numerically (RPC strips leading zeros; ethers zero-pads).
    const sig = decoded.signature;
    expect(sig, 'decoded tx is signed').to.not.equal(null);
    expect(BigInt(sig!.r), 'signature.r').to.equal(BigInt(txObject.r));
    expect(BigInt(sig!.s), 'signature.s').to.equal(BigInt(txObject.s));
    return decoded;
}

// ===========================================================================
// Block transaction-count endpoints (eth_getBlockTransactionCountBy*).
// ===========================================================================

/** eth_getBlockTransactionCountByHash wrapper. */
export function txCountByHash(provider: ethers.JsonRpcProvider, blockHash: string): Promise<string> {
    return provider.send('eth_getBlockTransactionCountByHash', [blockHash]);
}

/** eth_getBlockTransactionCountByNumber wrapper that hex-encodes a numeric height. */
export function txCountByNumber(
    provider: ethers.JsonRpcProvider,
    spec: BlockSpec,
): Promise<string> {
    const param = typeof spec === 'number' ? ethers.toQuantity(spec) : spec;
    return provider.send('eth_getBlockTransactionCountByNumber', [param]);
}

/** Assert a count value is a canonical QUANTITY and equals the expected number of txs. */
export function assertTxCount(value: any, expected: number, label = 'tx count'): void {
    expect(value, `${label} is a canonical quantity`).to.match(HEX_QUANTITY);
    expect(Number(BigInt(value)), label).to.equal(expected);
}

/**
 * Scan backwards from the chain head for a block with zero transactions. Sei mints empty
 * blocks continuously, so one is virtually always within a short lookback window. Returns
 * the height (and its hash) or undefined if none was found.
 */
export async function findEmptyBlock(
    provider: ethers.JsonRpcProvider,
    lookback = 60,
): Promise<{ number: number; hash: string } | undefined> {
    const head = await provider.getBlockNumber();
    for (let n = head; n >= 0 && n > head - lookback; n--) {
        const blk = await provider.send('eth_getBlockByNumber', [ethers.toQuantity(n), false]);
        if (blk && blk.transactions.length === 0) return { number: n, hash: blk.hash };
    }
    return undefined;
}

import { ethers } from 'ethers';
import { expect } from 'chai';
import { EvmAccount, abiOf, bytecodeOf, selfAuthorize } from './evmUtils';
import { RuntimeState, claimPool } from './testUtils';
import { generateSeiAddress } from './cosmosUtils';
import { prepareCw20Transfer } from './wasmUtils';
import { waitUntil } from './chainUtils';
import { HASH32, BLOOM256, NONCE8, HEX_QUANTITY, HEX_DATA, ADDRESS } from './format';
import { STAKING_PRECOMPILE_ADDRESS, USEI, ZERO_HASH } from './constants';
export { STAKING_PRECOMPILE_ADDRESS, USEI, ZERO_HASH };

/**
 * Shared fixtures + assertions for the eth_getBlockByNumber / eth_getBlockByHash parity specs.
 * Sei (a Cosmos chain) packs many transactions into one block, so we build a single "rich" block
 * carrying every EVM tx type (legacy / access-list / EIP-1559 / set-code), a contract deployment, a
 * contract call, plain EOA transfers and a precompile call — each from a *distinct* funded sender —
 * so we can verify each sender's gas + fee against the block. For the geth reference we send a
 * single transaction and assert the block/tx field schema matches.
 */

export const EMPTY_UNCLES_HASH =
    '0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347';

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

export const SEI_ONLY_BLOCK_FIELDS = ['totalDifficulty'] as const;
export const GETH_ONLY_BLOCK_FIELDS = [
    'blobGasUsed',
    'excessBlobGas',
    'parentBeaconBlockRoot',
    'requestsHash',
    'withdrawals',
    'withdrawalsRoot',
] as const;

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
    | 'precompile'
    | 'cw20Pointer'
    | 'outOfGas'
    | 'revertErc20';

export interface SentTx {
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
    gasLimit: bigint;
    /** Execution outcome: 1 = success, 0 = included-but-failed (revert / out-of-gas). */
    status: number;
    hash: string;
    receipt: ethers.TransactionReceipt;
}

export const ACCESS_LIST_FIXTURE = [
    { address: '0x' + '11'.repeat(20), storageKeys: ['0x' + '00'.repeat(32)] },
] as const;

export interface RichBlock {
    number: number;
    hash: string;
    txs: SentTx[];
    /**
     * Gas used by the co-located pointer-backed CW20 transfer (a pure Cosmos tx). Sei folds
     * this into the EVM receipts' cumulativeGasUsed (in tx-index order) but excludes it from
     * block.gasUsed; gas-accounting assertions use it to reconcile the one cumulative jump.
     * Absent on wasm-disabled chains.
     */
    cosmosShellGas?: bigint;
}

export interface RpcTx {
    hash: string;
    blockHash: string;
    blockNumber: string;
    transactionIndex: string;
    from: string;
    to: string | null;
    value: string;
    gas: string;
    gasPrice: string;
    nonce: string;
    type: string;
    input: string;
    r: string;
    s: string;
    v?: string;
    yParity?: string;
    chainId?: string;
    accessList?: unknown[];
    maxFeePerGas?: string;
    maxPriorityFeePerGas?: string;
    authorizationList?: unknown[];
}

export interface RpcBlock {
    number: string;
    hash: string;
    parentHash: string;
    nonce: string;
    sha3Uncles: string;
    logsBloom: string;
    transactionsRoot: string;
    stateRoot: string;
    receiptsRoot: string;
    mixHash: string;
    miner: string;
    difficulty: string;
    extraData: string;
    size: string;
    gasLimit: string;
    gasUsed: string;
    timestamp: string;
    baseFeePerGas: string;
    uncles: string[];
    transactions: (string | RpcTx)[];
    totalDifficulty?: string;
    withdrawals?: unknown[];
    withdrawalsRoot?: string;
}

/** A single JSON-RPC log object carried in a receipt / eth_getLogs result. */
export interface RpcLog {
    address: string;
    topics: string[];
    data: string;
    blockHash: string;
    blockNumber: string;
    transactionHash: string;
    transactionIndex: string;
    logIndex: string;
    removed: boolean;
}

/**
 * The JSON-RPC transaction-receipt object. `to` is creation-dependent (Sei omits it on a
 * creation receipt, geth returns null), and `contractAddress` is set iff it was a creation.
 */
export interface RpcReceipt {
    transactionHash: string;
    transactionIndex: string;
    blockHash: string;
    blockNumber: string;
    from: string;
    to?: string | null;
    cumulativeGasUsed: string;
    gasUsed: string;
    effectiveGasPrice: string;
    contractAddress: string | null;
    logs: RpcLog[];
    logsBloom: string;
    status: string;
    type: string;
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

async function waitForNextBlock(provider: ethers.JsonRpcProvider, label: string): Promise<void> {
    const start = await provider.getBlockNumber();
    await waitUntil(
        async () => ((await provider.getBlockNumber()) > start ? true : null),
        { timeoutMs: 10_000, intervalMs: 25, label },
    );
}

const TRANSFER_VALUE = ethers.parseEther('0.001');
const rand = (): string => ethers.Wallet.createRandom().address;

/** Minimal ERC20 ABI for calling a CW20's EVM pointer. */
const ERC20_POINTER_IFACE = new ethers.Interface([
    'function transfer(address to, uint256 amount) returns (bool)',
]);

/**
 * Broadcast one transaction of every kind, each from its own signer, and wait for
 * them to land in a single block. Retries the whole batch if the chain happens to
 * split them across blocks — each retry re-prices with a higher fee multiplier and
 * tip so the batch outbids its way into one block on a congested chain instead of
 * waiting the chain out. `signers` must hold at least 7 funded accounts.
 *
 * When the chain has wasm enabled (runtime.wasm is populated by the bootstrap), the
 * block additionally carries a dual-VM pair for the same CW20 token: an EVM `transfer`
 * through the token's ERC20 pointer (a real EVM tx, returned in `txs`) and a pure
 * Cosmos `MsgExecuteContract` CW20 transfer (NOT returned — it never surfaces over EVM
 * JSON-RPC — but required to co-locate in the same block). The batch retries until both
 * the EVM txs and the Cosmos tx land together.
 */
export async function buildRichSeiBlock(
    provider: ethers.JsonRpcProvider,
    runtime: RuntimeState,
    signers: EvmAccount[],
    attempts = 6,
): Promise<RichBlock> {
    if (signers.length < 9) {
        throw new Error(`buildRichSeiBlock needs >= 9 signers, got ${signers.length}`);
    }

    const wasm = runtime.wasm;
    const wasmActor = wasm ? EvmAccount.fromPrivateKey(wasm.actor.privateKey, provider) : undefined;
    // One throw-away recipient for the Cosmos CW20 transfer, reused across retries.
    const cosmosRecipient = wasm ? await generateSeiAddress() : undefined;
    const erc20Iface = new ethers.Interface(abiOf('TestERC20.sol', 'TestERC20'));
    const erc20Bytecode = bytecodeOf('TestERC20.sol', 'TestERC20');
    const gasBurnerIface = new ethers.Interface(abiOf('GasBurner.sol', 'RealGasBurner'));
    const validatorsData = new ethers.Interface([
        'function validators(string status, bytes pagination) returns (bytes,bytes)',
    ]).encodeFunctionData('validators', ['BOND_STATUS_BONDED', '0x']);

    let lastErr: unknown;
    for (let attempt = 0; attempt < attempts; attempt++) {
        const p = await pricing(provider, BigInt(3 + attempt * 2), BigInt(1 + attempt));
        const [sLegacy, sAccess, s1559, sSetCode, sDeploy, sErc20, sPrecompile, sOutOfGas, sRevert] =
            signers;
        const [nLegacy, nAccess, n1559, nSetCode, nDeploy, nErc20, nPrecompile, nOutOfGas, nRevert] =
            await Promise.all(signers.map(s => s.nonce('pending')));
        const auth = await selfAuthorize(sSetCode, runtime.contracts.simpleAccount7702);

        // Legacy + access-list pay via gasPrice; every other kind via the 1559 fee caps.
        const legacyFee = { gasPrice: p.gasPrice };
        const dynFee = {
            maxFeePerGas: p.maxFeePerGas,
            maxPriorityFeePerGas: p.maxPriorityFeePerGas,
        };

        const specs: { kind: TxKind; signer: EvmAccount; req: ethers.TransactionRequest }[] = [
            {
                kind: 'legacy',
                signer: sLegacy,
                req: { type: 0, to: rand(), value: TRANSFER_VALUE, gasLimit: 21000n, nonce: nLegacy, ...legacyFee },
            },
            {
                kind: 'accessList',
                signer: sAccess,
                req: {
                    type: 1,
                    to: rand(),
                    value: TRANSFER_VALUE,
                    accessList: ACCESS_LIST_FIXTURE as any,
                    gasLimit: 30000n,
                    nonce: nAccess,
                    ...legacyFee,
                },
            },
            {
                kind: 'eip1559',
                signer: s1559,
                req: { type: 2, to: rand(), value: TRANSFER_VALUE, gasLimit: 21000n, nonce: n1559, ...dynFee },
            },
            {
                kind: 'setCode',
                signer: sSetCode,
                req: {
                    type: 4,
                    to: sSetCode.address,
                    data: '0x',
                    authorizationList: [auth],
                    gasLimit: 200000n,
                    nonce: nSetCode,
                    ...dynFee,
                },
            },
            {
                kind: 'deploy',
                signer: sDeploy,
                req: {
                    type: 2,
                    data: ethers.concat([erc20Bytecode, erc20Iface.encodeDeploy([sDeploy.address])]),
                    gasLimit: 1_500_000n,
                    nonce: nDeploy,
                    ...dynFee,
                },
            },
            {
                kind: 'erc20',
                signer: sErc20,
                req: {
                    type: 2,
                    to: runtime.contracts.erc20,
                    data: erc20Iface.encodeFunctionData('transfer', [rand(), 0n]),
                    gasLimit: 120000n,
                    nonce: nErc20,
                    ...dynFee,
                },
            },
            {
                kind: 'precompile',
                signer: sPrecompile,
                req: {
                    type: 2,
                    to: STAKING_PRECOMPILE_ADDRESS,
                    data: validatorsData,
                    gasLimit: 2_000_000n,
                    nonce: nPrecompile,
                    ...dynFee,
                },
            },
            // Two included-but-failed txs that co-locate in the same block (status 0):
            //  (1) out-of-gas — a heavy gasBurner call capped well below what it needs, so the
            //      EVM consumes the entire gas limit (gasUsed == gasLimit, no refund, no logs).
            {
                kind: 'outOfGas',
                signer: sOutOfGas,
                req: {
                    type: 2,
                    to: runtime.contracts.gasBurner,
                    data: gasBurnerIface.encodeFunctionData('burnGasIterations', [
                        BigInt(attempt),
                        1_000_000n,
                    ]),
                    gasLimit: 100_000n,
                    nonce: nOutOfGas,
                    ...dynFee,
                },
            },
            //  (2) ERC20 insufficient balance — a transfer far larger than the (zero) token
            //      balance, so the contract's require reverts (status 0, unused gas refunded,
            //      no Transfer log emitted).
            {
                kind: 'revertErc20',
                signer: sRevert,
                req: {
                    type: 2,
                    to: runtime.contracts.erc20,
                    data: erc20Iface.encodeFunctionData('transfer', [
                        rand(),
                        ethers.parseEther('1000000000'),
                    ]),
                    gasLimit: 120_000n,
                    nonce: nRevert,
                    ...dynFee,
                },
            },
        ];

        if (wasm && wasmActor) {
            const nActor = await wasmActor.nonce('pending');
            specs.push({
                kind: 'cw20Pointer',
                signer: wasmActor,
                req: {
                    type: 2,
                    to: wasm.cw20Pointer,
                    data: ERC20_POINTER_IFACE.encodeFunctionData('transfer', [runtime.funded.admin, 1n]),
                    gasLimit: 1_000_000n,
                    nonce: nActor,
                    ...dynFee,
                },
            });
        }

        try {
            const preparedCosmos = wasm
                ? await prepareCw20Transfer(
                      wasm.cw20,
                      cosmosRecipient!,
                      '1',
                      runtime.funded.adminMnemonic,
                  )
                : undefined;
            const populated = await Promise.all(
                specs.map(s => s.signer.wallet.populateTransaction(s.req)),
            );
            const signed = await Promise.all(
                populated.map((tx, i) => specs[i].signer.wallet.signTransaction(tx)),
            );

            // Stage both sides before a fresh block boundary, then broadcast the pure Cosmos
            // CW20 transfer and the EVM batch together. Broadcasting the EVM txs first lets a
            // fast proposer seal them one height before the Cosmos tx, which made the rich-block
            // fixture flaky.
            if (wasm) {
                await waitForNextBlock(
                    provider,
                    `next Sei block before rich batch attempt ${attempt + 1}`,
                );
            }
            const cosmosPending = preparedCosmos
                ? preparedCosmos
                      .broadcast()
                      .then(p => ({ hash: p.hash, wait: p.wait.catch(() => null) }))
                      .catch(() => null)
                : Promise.resolve(null);
            const evmBroadcasts = Promise.all(signed.map(raw => provider.broadcastTransaction(raw)));
            let pendingCosmos: Awaited<typeof cosmosPending>;
            let responses: ethers.TransactionResponse[];
            try {
                [pendingCosmos, responses] = await Promise.all([cosmosPending, evmBroadcasts]);
            } catch (e) {
                pendingCosmos = await cosmosPending;
                // If the Cosmos tx passed CheckTx, wait for its sequence to settle before retrying.
                if (pendingCosmos) await pendingCosmos.wait;
                throw e;
            }
            // waitForTransaction (unlike resp.wait) does NOT throw on a status-0 receipt, so the
            // two included-but-failed txs resolve to their receipts instead of aborting the batch.
            const receipts = await Promise.all(
                responses.map(r => provider.waitForTransaction(r.hash, 1, 25_000)),
            );
            const cosmos = pendingCosmos ? await pendingCosmos.wait : null;
            const blockNumbers = receipts.map(r => r!.blockNumber);
            const uniqueBlocks = new Set(blockNumbers);
            const allOk = receipts.every(r => r && (r.status === 1 || r.status === 0));
            // When wasm is on, also require the Cosmos CW20 transfer to co-locate with the
            // EVM batch's (single) block; otherwise re-price and retry the whole pair.
            const cosmosCoLocated =
                !wasm || (cosmos !== null && cosmos.code === 0 && cosmos.height === blockNumbers[0]);
            if (uniqueBlocks.size === 1 && allOk && cosmosCoLocated) {
                const blockNumber = blockNumbers[0];
                const block = await provider.getBlock(blockNumber);
                const txs: SentTx[] = specs.map((s, i) => ({
                    kind: s.kind,
                    type: s.req.type as number,
                    sender: s.signer.address,
                    to: (s.req.to as string | undefined) ?? null,
                    data: (s.req.data as string | undefined) ?? '0x',
                    value: (s.req.value as bigint | undefined) ?? 0n,
                    nonce: s.req.nonce as number,
                    gasPrice: s.req.gasPrice as bigint | undefined,
                    maxFeePerGas: s.req.maxFeePerGas as bigint | undefined,
                    maxPriorityFeePerGas: s.req.maxPriorityFeePerGas as bigint | undefined,
                    gasLimit: s.req.gasLimit as bigint,
                    status: receipts[i]!.status ?? 1,
                    hash: responses[i].hash,
                    receipt: receipts[i] as ethers.TransactionReceipt,
                }));
                return {
                    number: blockNumber,
                    hash: block!.hash!,
                    txs,
                    cosmosShellGas: wasm && cosmos ? cosmos.gasUsed : undefined,
                };
            }
            lastErr = new Error(
                `attempt ${attempt + 1}: EVM blocks ${[...uniqueBlocks].join(',')}` +
                    (wasm
                        ? `, cosmos cw20 ${cosmos ? `code ${cosmos.code} @ block ${cosmos.height}` : 'failed'} ` +
                          `(EVM block ${blockNumbers[0]})`
                        : ''),
            );
        } catch (e) {
            lastErr = e;
        }
    }
    throw new Error(`buildRichSeiBlock: could not pack one block after ${attempts} attempts: ${lastErr}`);
}

// The serial runner (.mocharc.run.json) loads every spec into a single process, so this
// module-level cache builds the expensive rich block — one block packed with a transaction of
// every type, each from its own funded pool account — exactly ONCE for the whole suite, re-asserted
// by every spec that needs it (block, receipt, logs and tx-lookup). It claims its own 7 pool
// accounts on first use. (Under the parallel runner each shard builds its own copy.)
let cachedRichBlock: RichBlock | undefined;
let cachedRichBlockPromise: Promise<RichBlock> | undefined;

export async function sharedRichBlock(
    provider: ethers.JsonRpcProvider,
    runtime: RuntimeState,
): Promise<RichBlock> {
    if (cachedRichBlock) return cachedRichBlock;
    // Guard against two specs' `before` hooks racing the first build in the same process.
    if (!cachedRichBlockPromise) {
        cachedRichBlockPromise = (async () => {
            const signers = claimPool(runtime, provider, 9, 'shared-rich-block');
            cachedRichBlock = await buildRichSeiBlock(provider, runtime, signers);
            return cachedRichBlock;
        })().catch(e => {
            cachedRichBlockPromise = undefined;
            throw e;
        });
    }
    return cachedRichBlockPromise;
}

/**
 * The rich block's two included-but-failed transactions (status 0), co-located with the
 * successful ones: an out-of-gas contract call and an ERC20 transfer that reverts on its
 * balance check.
 */
export function richFailedTxs(rich: RichBlock): { outOfGas: SentTx; revertErc20: SentTx } {
    return {
        outOfGas: rich.txs.find(t => t.kind === 'outOfGas')!,
        revertErc20: rich.txs.find(t => t.kind === 'revertErc20')!,
    };
}

/** Rich-block tx kinds that are built to fail (receipt status 0x0); every other kind succeeds. */
export const FAILING_TX_KINDS = new Set<TxKind>(['outOfGas', 'revertErc20']);

/** The receipt status a rich-block tx MUST report, derived from its kind (the intended outcome). */
export function expectedStatusHex(kind: TxKind): '0x0' | '0x1' {
    return FAILING_TX_KINDS.has(kind) ? '0x0' : '0x1';
}

/**
 * Assert a JSON-RPC receipt for an included-but-failed tx: reverted status, gas burned, no
 * logs, and the gas-used signature of its failure mode — an out-of-gas call consumes the
 * entire gas limit (no refund), whereas a require/revert refunds the unused gas.
 */
export function assertFailedReceipt(rc: any, sent: SentTx): void {
    expect(rc, `receipt exists for ${sent.kind}`).to.not.equal(null);
    expect(rc.status, `${sent.kind} reverted (status 0x0)`).to.equal('0x0');
    expect(BigInt(rc.gasUsed) > 0n, `${sent.kind} still burned gas`).to.equal(true);
    expect(rc.logs, `${sent.kind} emitted no logs`).to.be.an('array').that.has.lengthOf(0);
    if (sent.kind === 'outOfGas') {
        expect(BigInt(rc.gasUsed), 'out-of-gas consumes the entire gas limit').to.equal(
            sent.gasLimit,
        );
    } else {
        expect(BigInt(rc.gasUsed) < sent.gasLimit, 'a revert refunds the unused gas').to.equal(true);
    }
}

/** Assemble the SentTx record for a broadcast type-2 transaction. */
function sentTx1559(
    kind: TxKind,
    signer: EvmAccount,
    p: { maxFeePerGas: bigint; maxPriorityFeePerGas: bigint },
    fields: { to: string | null; data: string; value: bigint },
    resp: ethers.TransactionResponse,
    receipt: ethers.TransactionReceipt,
): SentTx {
    return {
        kind,
        type: 2,
        sender: signer.address,
        to: fields.to,
        data: fields.data,
        value: fields.value,
        nonce: resp.nonce,
        maxFeePerGas: p.maxFeePerGas,
        maxPriorityFeePerGas: p.maxPriorityFeePerGas,
        gasLimit: resp.gasLimit,
        status: receipt.status ?? 1,
        hash: resp.hash,
        receipt,
    };
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
        tx: sentTx1559('eip1559', signer, p, { to, data: '0x', value }, resp, receipt),
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
    return sentTx1559('erc20', signer, p, { to: erc20Address, data, value: 0n }, resp, receipt);
}

/**
 * Sign (but do not broadcast) a well-formed legacy transaction whose gas limit is
 * below the 21000 intrinsic floor. Submitting it must be *rejected* pre-execution by both
 * nodes (same -32000 code): geth with a descriptive "intrinsic gas too low", Sei with a
 * generic ABCI error from its ante (a documented divergence). Returns the raw payload + hash.
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
export function assertCanonicalHeader(block: RpcBlock, opts: { hasTxs: boolean }): void {
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
export function assertCanonicalTx(
    tx: RpcTx,
    block: { hash: string; number: string | number },
): void {
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
 * Per-type transaction-object schema as the typed-transaction EIPs require geth to
 * serialize it in block/tx responses. Asserts both REQUIRED and FORBIDDEN type-specific
 * fields, so a node that over-populates (e.g. emits maxFeePerGas on a legacy tx) or
 * under-populates (e.g. drops authorizationList on a 7702 tx) is caught.
 *
 *   legacy (0x0): gasPrice;  NO accessList / maxFee* / yParity / authorizationList
 *   2930  (0x1):  gasPrice + accessList + yParity;  NO maxFee* / authorizationList
 *   1559  (0x2):  maxFee* + accessList + yParity (+ effective gasPrice);  NO authorizationList
 *   7702  (0x4):  maxFee* + accessList + yParity + authorizationList
 */
export function assertTxTypeSchema(tx: RpcTx): void {
    const type = Number(BigInt(tx.type));
    const has = (f: string) => expect(tx, `type ${type} tx must expose ${f}`).to.have.property(f);
    const lacks = (f: string) =>
        expect(f in tx, `type ${type} tx must NOT expose ${f}`).to.equal(false);

    has('gasPrice'); // present on every type (effective price for 1559/7702)
    switch (type) {
        case 0:
            lacks('accessList');
            lacks('maxFeePerGas');
            lacks('maxPriorityFeePerGas');
            lacks('yParity');
            lacks('authorizationList');
            break;
        case 1:
            has('accessList');
            has('yParity');
            lacks('maxFeePerGas');
            lacks('maxPriorityFeePerGas');
            lacks('authorizationList');
            break;
        case 2:
            has('accessList');
            has('yParity');
            has('maxFeePerGas');
            has('maxPriorityFeePerGas');
            lacks('authorizationList');
            break;
        case 4:
            has('accessList');
            has('yParity');
            has('maxFeePerGas');
            has('maxPriorityFeePerGas');
            has('authorizationList');
            expect(tx.authorizationList, 'authorizationList is an array').to.be.an('array');
            break;
        default:
            throw new Error(`assertTxTypeSchema: unexpected tx type ${type}`);
    }
    if (type >= 1) expect(tx.accessList, 'accessList is an array').to.be.an('array');
}

/** Fetch the receipt for every transaction listed in a block (hash- or object-form tx lists). */
function receiptsForBlock(
    provider: ethers.JsonRpcProvider,
    block: Pick<RpcBlock, 'transactions'>,
): Promise<(ethers.TransactionReceipt | null)[]> {
    const hashes = block.transactions.map(t => (typeof t === 'string' ? t : t.hash));
    return Promise.all(hashes.map(h => provider.getTransactionReceipt(h)));
}

/**
 * Reconcile the EVM-visible (eth_*) cumulativeGasUsed series of a single block.
 *
 * Receipts must be the eth-visible ones (EVM txs only), pre-mapped to native bigints and
 * sorted by transaction index. `blockGasUsed` is the eth block.gasUsed (which sums ONLY
 * EVM receipt gas). `cosmosShellGas`, when given, is the gas of a co-located pointer-backed
 * CW20 transfer (a pure Cosmos tx): Sei folds that Cosmos-unit gas into the EVM receipts'
 * cumulativeGasUsed in tx-index order, but NOT into block.gasUsed.
 *
 * The series therefore tracks the running Σ of EVM gasUsed exactly, EXCEPT for a single
 * step where the shell receipt sits just before an EVM tx — there cumulativeGasUsed jumps
 * by an extra `cosmosShellGas`. We assert that jump equals the Cosmos tx's gas exactly,
 * that it happens at most once (the rich block has one CW20 tx; it may instead sort last,
 * in which case no jump appears), and that every other step accumulates cleanly.
 */
export function assertCumulativeGasSeries(
    ordered: { index: number; gasUsed: bigint; cumulativeGasUsed: bigint }[],
    blockGasUsed: bigint,
    cosmosShellGas?: bigint,
): void {
    let prevCumulative = 0n;
    let runningOwn = 0n;
    let shellOffset = 0n;
    let jumps = 0;
    for (const r of ordered) {
        expect(r.gasUsed > 0n, `receipt ${r.index} burned gas`).to.equal(true);
        runningOwn += r.gasUsed;
        const expected = runningOwn + shellOffset;
        if (r.cumulativeGasUsed !== expected) {
            // The only legitimate perturbation is the shell receipt of the co-located CW20
            // transfer folded in just before this EVM tx. Pin the jump to that exact gas.
            const extra = r.cumulativeGasUsed - expected;
            expect(
                cosmosShellGas !== undefined,
                `unexpected cumulativeGasUsed jump of ${extra} at idx ${r.index} with no CW20 shell tx`,
            ).to.equal(true);
            expect(extra, `cumulative jump at idx ${r.index} == co-located CW20 shell gas`).to.equal(
                cosmosShellGas,
            );
            shellOffset += extra;
            jumps++;
        }
        expect(r.cumulativeGasUsed > prevCumulative, `cumulativeGasUsed strictly increasing at idx ${r.index}`).to.equal(
            true,
        );
        prevCumulative = r.cumulativeGasUsed;
    }
    expect(jumps, 'at most one CW20 shell receipt perturbs the EVM cumulative series').to.be.at.most(1);
    
    expect(
        prevCumulative,
        'final cumulativeGasUsed == block.gasUsed + any folded CW20 shell gas',
    ).to.equal(blockGasUsed + shellOffset);
    
}

/**
 * Verify a Sei block's gas accounting against the EVM-visible (eth_*) receipts.
 *
 * `block.gasUsed` on the eth namespace sums ONLY EVM-transaction receipt gas (evmrpc
 * excludes shell receipts from the block), so `block.gasUsed == Σ eth-visible gasUsed`
 * holds exactly. The cumulativeGasUsed series is reconciled via assertCumulativeGasSeries,
 * which also asserts that a co-located CW20 transfer's gas (`cosmosShellGas`) is exactly
 * what perturbs the series — proving the Cosmos tx's gas does reach EVM cumulativeGasUsed.
 */
export async function assertGasAccounting(
    provider: ethers.JsonRpcProvider,
    block: Pick<RpcBlock, 'transactions' | 'gasUsed'>,
    cosmosShellGas?: bigint,
): Promise<void> {
    const receipts = await receiptsForBlock(provider, block);
    const summed = receipts.reduce((acc, r) => acc + r!.gasUsed, 0n);
    expect(summed, 'block.gasUsed == Σ EVM-visible receipt.gasUsed').to.equal(BigInt(block.gasUsed));

    const ordered = [...receipts]
        .sort((a, b) => a!.index - b!.index)
        .map(r => ({ index: r!.index, gasUsed: r!.gasUsed, cumulativeGasUsed: r!.cumulativeGasUsed }));
    assertCumulativeGasSeries(ordered, BigInt(block.gasUsed), cosmosShellGas);
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
    const receipts = await receiptsForBlock(provider, block);
    const expected = computeLogsBloom(receipts.filter((r): r is ethers.TransactionReceipt => !!r));
    expect(block.logsBloom, 'logsBloom == Bloom(all emitted logs)').to.equal(expected);
}

export interface BurnGasBurstOpts {
    /** Number of burst rounds (one block's worth of txs each). */
    rounds?: number;
    /** Per-tx gas limit (gas wanted). Lower values pack more txs per block. */
    gasLimit?: bigint;
    /** burnGasIterations rounds per tx; ~22.1k gas each. */
    iterations?: bigint;
    /** Priority fee every burst tx pays (so a congested block's 50th-pct reward == this). */
    tip?: bigint;
    /** Hook run after each round, e.g. to sample live chain state under load. */
    onRound?: (round: number) => Promise<void>;
}

/**
 * Deliberately raise the base fee: fire repeated heavy gas-burner transactions from
 * every signer until the chain has produced blocks above its gas target. Returns the
 * pre-burst base fee and the block range the burst landed in so a test can verify the
 * baseFeePerGas the block reports afterwards. Mirrors eth_feeHistory's burnBurst.
 *
 * The defaults bump the base fee; pass a tighter `gasLimit` to instead pack enough txs
 * into one block to cross the 80% gas-used congestion threshold (see sampleCongestedTip).
 */
export async function burnGasBurst(
    provider: ethers.JsonRpcProvider,
    runtime: RuntimeState,
    signers: EvmAccount[],
    opts: BurnGasBurstOpts = {},
): Promise<{ beforeBaseFee: bigint; minBlock: number; maxBlock: number }> {
    const {
        rounds = 12,
        gasLimit = 6_000_000n,
        iterations = 200n,
        tip = ethers.parseUnits('2', 'gwei'),
        onRound,
    } = opts;
    const burnerIface = new ethers.Interface(abiOf('GasBurner.sol', 'RealGasBurner'));
    const burner = runtime.contracts.gasBurner;
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
            if ((await s.balance()) < gasLimit * maxFee) continue;
            const data = burnerIface.encodeFunctionData('burnGasIterations', [
                BigInt(round * 100 + i),
                iterations,
            ]);
            sends.push(
                s.wallet
                    .sendTransaction({
                        to: burner,
                        data,
                        gasLimit,
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
        // Optional per-round hook so callers can sample live chain state (e.g. eth_gasPrice)
        // while the base fee is still elevated by the load just applied.
        if (onRound) await onRound(round);
    }
    return { beforeBaseFee, minBlock, maxBlock };
}
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
export function blockReceipts(provider: ethers.JsonRpcProvider, spec: BlockSpec): Promise<RpcReceipt[]> {
    const param = typeof spec === 'number' ? ethers.toQuantity(spec) : spec;
    return provider.send('eth_getBlockReceipts', [param]);
}

/** Every receipt field is present, canonically encoded and linked to its block + index. */
export function assertCanonicalReceipt(
    rc: RpcReceipt,
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
export function assertRawTxMatches(raw: string, txObject: RpcTx): ethers.Transaction {
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
            BigInt(txObject.maxPriorityFeePerGas!),
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

/** A signed-but-unbroadcast transaction plus the fields a spec asserts after submitting it. */
export interface SignedRawTx {
    raw: string;
    hash: string;
    type: 0 | 1 | 2;
    to: string;
    value: bigint;
    nonce: number;
}

/**
 * Sign (offline, never broadcast) a self-contained transfer of the requested EIP-2718 type,
 * priced to land promptly. Returns the raw bytes + keccak256 hash so eth_sendRawTransaction
 * specs can submit the canonical encoding of every tx type and prove the node accepts it.
 */
export async function signRawTransfer(
    provider: ethers.JsonRpcProvider,
    signer: EvmAccount,
    type: 0 | 1 | 2,
    overrides: Partial<{ nonce: number; to: string; value: bigint; gasLimit: bigint }> = {},
): Promise<SignedRawTx> {
    const [net, pendingNonce, p] = await Promise.all([
        provider.getNetwork(),
        signer.nonce('pending'),
        pricing(provider),
    ]);
    const nonce = overrides.nonce ?? pendingNonce;
    const to = overrides.to ?? rand();
    const value = overrides.value ?? TRANSFER_VALUE;
    const fee =
        type === 2
            ? { maxFeePerGas: p.maxFeePerGas, maxPriorityFeePerGas: p.maxPriorityFeePerGas }
            : { gasPrice: p.gasPrice };
    // A type-1 access list raises the intrinsic floor above 21000 (2400/address + 1900/key),
    // so give it headroom; a plain transfer needs exactly 21000.
    const gasLimit = overrides.gasLimit ?? (type === 1 ? 30000n : 21000n);
    const raw = await signer.wallet.signTransaction({
        to,
        value,
        nonce,
        gasLimit,
        chainId: net.chainId,
        type,
        accessList: type === 1 ? (ACCESS_LIST_FIXTURE as any) : undefined,
        ...fee,
    });
    return { raw, hash: ethers.keccak256(raw), type, to, value, nonce };
}

export function sendRaw(provider: ethers.JsonRpcProvider, raw: string): Promise<string> {
    return provider.send('eth_sendRawTransaction', [raw]);
}

/** eth_getVMError wrapper: the stored VM error string for a tx (a Sei-specific method). */
export function getVmError(provider: ethers.JsonRpcProvider, hash: string): Promise<string> {
    return provider.send('eth_getVMError', [hash]);
}

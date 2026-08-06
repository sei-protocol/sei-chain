import crypto from 'crypto';
import path from 'path';
import { ethers } from 'ethers';
import { AuthInfo, TxBody, TxRaw } from 'cosmjs-types/cosmos/tx/v1beta1/tx';
import {
    EVM_MESSAGE_TYPE,
    PACIFIC_COSMOS_CHAIN_ID,
    PACIFIC_EVM_CHAIN_ID,
    REPLAY_SCHEMA_VERSION,
    ReplayBlock,
    ReplayCosmosTransaction,
    ReplayEvmTransaction,
    ReplaySegment,
    ReplayTraceSummary,
} from './replayTypes';
import { correlateEvmWrapper } from './evmCorrelation';
import {
    normalizeCallTrace,
    summarizePrestateDiff,
    summarizeStructLogs,
    TraceCaptureMode,
} from './traceCapture';
import { mapConcurrent } from '../concurrency';
import { writeJsonAtomic } from '../io';

// Capture uses a hand-rolled fetch client instead of ethers.JsonRpcProvider or
// @cosmjs/tendermint-rpc because both sides rely on HTTP-level JSON-RPC batch
// arrays (one request per block range), which neither library issues, and the
// retry/backoff policy must treat a whole batch as one atomic unit.

interface RpcRequest {
    jsonrpc: '2.0';
    id: number;
    method: string;
    params: unknown[];
}

interface RpcResponse<T> {
    id: number;
    result?: T;
    error?: { code: number; message: string };
}

export function orderRpcBatchResponses<T>(
    requests: Pick<RpcRequest, 'id'>[],
    responses: RpcResponse<T>[],
): RpcResponse<T>[] {
    const expectedIds = new Set(requests.map(request => request.id));
    const byId = new Map<number, RpcResponse<T>>();
    for (const item of responses) {
        if (!expectedIds.has(item.id) || byId.has(item.id)) {
            throw new Error(`EVM RPC returned unexpected or duplicate batch id ${item.id}`);
        }
        byId.set(item.id, item);
    }
    return requests.map(request => {
        const item = byId.get(request.id);
        if (!item) throw new Error(`EVM RPC omitted batch id ${request.id}`);
        return item;
    });
}

interface RpcTransaction {
    hash: string;
    blockNumber: string;
    transactionIndex: string;
    from: string;
    to: string | null;
    nonce: string;
    chainId?: string;
    type?: string;
    input: string;
    value: string;
    gas: string;
    gasPrice?: string;
    maxFeePerGas?: string;
    maxPriorityFeePerGas?: string;
    accessList?: unknown[];
    r?: string;
    s?: string;
    v?: string;
    yParity?: string;
}

interface RpcBlock {
    number: string;
    hash: string;
    parentHash: string;
    timestamp: string;
    gasLimit: string;
    gasUsed: string;
    baseFeePerGas?: string;
    transactions: RpcTransaction[];
}

interface RpcReceipt {
    transactionHash: string;
    gasUsed: string;
    effectiveGasPrice?: string;
    status?: string;
    contractAddress?: string | null;
    logs?: unknown[];
}

interface CosmosRpcRequest {
    jsonrpc: '2.0';
    id: string;
    method: 'block' | 'block_results';
    params: { height: string };
}

interface CosmosBlockResult {
    block_id: { hash: string };
    block: {
        header: {
            height: string;
            chain_id: string;
            time: string;
            last_block_id?: { hash?: string };
        };
        data: { txs?: string[] | null };
    };
}

interface CosmosDeliverTx {
    code?: number;
    gas_wanted?: string;
    gas_used?: string;
    events?: unknown[];
}

interface CosmosBlockResultsResult {
    height: string;
    txs_results?: CosmosDeliverTx[] | null;
}

interface CosmosBlockData {
    height: number;
    hash: string;
    parentHash: string;
    chainId: string;
    timestamp: number;
    txs: string[];
    results: CosmosDeliverTx[];
}

export interface PacificSourceOptions {
    evmRpcUrl: string;
    cosmosRpcUrl: string;
    evmConcurrency?: number;
    cosmosConcurrency?: number;
    blocksPerBatch?: number;
    maxRetries?: number;
    traceCaptureMode?: TraceCaptureMode;
    traceConcurrency?: number;
    traceMaxDepth?: number;
    traceMaxFrames?: number;
    traceTimeoutMs?: number;
    traceMaxRetries?: number;
}

export class PacificSource {
    readonly evmRpcUrl: string;
    readonly cosmosRpcUrl: string;
    private readonly evmConcurrency: number;
    private readonly cosmosConcurrency: number;
    private readonly blocksPerBatch: number;
    private readonly maxRetries: number;
    private readonly traceCaptureMode: TraceCaptureMode;
    private readonly traceConcurrency: number;
    private readonly traceMaxDepth: number;
    private readonly traceMaxFrames: number;
    private readonly traceTimeoutMs: number;
    private readonly traceMaxRetries: number;
    private nextRpcId = 1;

    constructor(options: PacificSourceOptions) {
        this.evmRpcUrl = options.evmRpcUrl;
        this.cosmosRpcUrl = options.cosmosRpcUrl;
        this.evmConcurrency = options.evmConcurrency ?? 2;
        this.cosmosConcurrency = options.cosmosConcurrency ?? 6;
        this.blocksPerBatch = options.blocksPerBatch ?? 20;
        if (this.blocksPerBatch < 1 || this.blocksPerBatch > 20) {
            throw new Error('blocksPerBatch must be between 1 and 20');
        }
        this.maxRetries = options.maxRetries ?? 10;
        this.traceCaptureMode = options.traceCaptureMode ?? 'calls';
        this.traceConcurrency = options.traceConcurrency ?? 1;
        this.traceMaxDepth = options.traceMaxDepth ?? 8;
        this.traceMaxFrames = options.traceMaxFrames ?? 64;
        this.traceTimeoutMs = options.traceTimeoutMs ?? 30_000;
        this.traceMaxRetries = options.traceMaxRetries ?? 3;
    }

    async verifyChain(): Promise<void> {
        const chainId = hexNumber(await this.rpc<string>('eth_chainId'));
        if (chainId !== PACIFIC_EVM_CHAIN_ID) {
            throw new Error(
                `Refusing EVM chain ${chainId}; expected Pacific-1 (${PACIFIC_EVM_CHAIN_ID})`,
            );
        }
    }

    async latestHeight(): Promise<number> {
        return hexNumber(await this.rpc<string>('eth_blockNumber'));
    }

    async blockTimestamp(height: number): Promise<number> {
        const block = await this.getBlock(height, false);
        return hexNumber(block.timestamp);
    }

    async findFirstBlockAtOrAfter(latest: number, targetTimestamp: number): Promise<number> {
        let distance = 1_000;
        let low = Math.max(1, latest - distance);
        while ((await this.blockTimestamp(low)) > targetTimestamp && low > 1) {
            distance *= 2;
            low = Math.max(1, latest - distance);
        }
        let high = latest;
        while (low < high) {
            const middle = Math.floor((low + high) / 2);
            if ((await this.blockTimestamp(middle)) < targetTimestamp) low = middle + 1;
            else high = middle;
        }
        return low;
    }

    async captureSegment(start: number, end: number, tipLag: number): Promise<ReplaySegment> {
        if (!Number.isInteger(start) || !Number.isInteger(end) || start <= 0 || end < start) {
            throw new Error(`Invalid capture range ${start}..${end}`);
        }

        const ranges = blockRanges(start, end, this.blocksPerBatch);
        const evmChunks = await mapConcurrent(ranges, this.evmConcurrency, range =>
            this.fetchEvmChunk(range.start, range.end),
        );
        const cosmosRanges = blockRanges(start, end, 5);
        const cosmosChunks = await mapConcurrent(cosmosRanges, this.cosmosConcurrency, range =>
            this.fetchCosmosChunk(range.start, range.end),
        );
        const evmBlocks = evmChunks
            .flatMap(chunk => chunk.blocks)
            .sort((a, b) => hexNumber(a.number) - hexNumber(b.number));
        const receipts = new Map<string, RpcReceipt>();
        for (const chunk of evmChunks) {
            for (const [hash, receipt] of chunk.receipts) receipts.set(hash, receipt);
        }
        const cosmosBlocks = cosmosChunks.flat().sort((a, b) => a.height - b.height);

        validateBlockCoverage(evmBlocks, cosmosBlocks, start, end);
        const codeHashes = await this.fetchRecipientCodeHashes(evmBlocks, end);
        const blocks: ReplayBlock[] = [];
        let canonicalTransactions = 0;
        let evmTransactions = 0;
        let wrappedEvmTransactions = 0;
        let linkedEvmTransactions = 0;
        let unresolvedEvmWrappers = 0;
        let unlinkedEvmTransactions = 0;
        let sourceBytes = 0;

        for (let offset = 0; offset < evmBlocks.length; offset++) {
            const evmBlock = evmBlocks[offset];
            const cosmosBlock = cosmosBlocks[offset];
            validateBlockPair(evmBlock, cosmosBlock, offset === 0 ? undefined : blocks[offset - 1]);

            const enrichedEvm = evmBlock.transactions.map(transaction => {
                const receipt = receipts.get(transaction.hash.toLowerCase());
                if (!receipt) {
                    throw new Error(
                        `Missing receipt for ${transaction.hash} in block ${cosmosBlock.height}`,
                    );
                }
                return toReplayEvmTransaction(transaction, receipt, codeHashes);
            });
            await this.attachCreationRuntime(enrichedEvm, cosmosBlock.height);
            await this.attachTraces(enrichedEvm);
            for (const transaction of enrichedEvm) {
                if (transaction.kind !== 'contractCreation') continue;
                transaction.creationMethod =
                    transaction.trace?.calls?.frames[0]?.type === 'CREATE2' ? 'CREATE2' : 'CREATE';
            }
            const decodedCosmos = decodeCosmosTransactions(cosmosBlock);
            const wrapped = decodedCosmos.filter(transaction => transaction.isEvm);
            const evmByHash = new Map(
                enrichedEvm.map(transaction => [transaction.hash.toLowerCase(), transaction]),
            );
            const linkedHashes = new Set<string>();
            for (const transaction of wrapped) {
                const evmMessages = transaction.messages.filter(
                    message => message.typeUrl === EVM_MESSAGE_TYPE,
                );
                if (evmMessages.length !== 1) {
                    transaction.evmLink = {
                        method: 'unresolved',
                        reason: `Expected one EVM message, received ${evmMessages.length}`,
                    };
                    unresolvedEvmWrappers++;
                    continue;
                }
                const link = correlateEvmWrapper(Buffer.from(evmMessages[0].valueBase64, 'base64'));
                transaction.evmLink = link;
                if (!link.hash) {
                    unresolvedEvmWrappers++;
                    continue;
                }
                const evm = evmByHash.get(link.hash);
                if (!evm) {
                    // Ante-failed wrappers can be canonical Cosmos transactions
                    // without an EVM block entry. Preserve the confirmed signed
                    // hash but do not invent a block-order match.
                    continue;
                }
                transaction.evm = evm;
                linkedHashes.add(link.hash);
                linkedEvmTransactions++;
            }
            const unlinked = enrichedEvm
                .filter(transaction => !linkedHashes.has(transaction.hash.toLowerCase()))
                .sort((left, right) => left.transactionIndex - right.transactionIndex);

            canonicalTransactions += decodedCosmos.length;
            evmTransactions += enrichedEvm.length;
            wrappedEvmTransactions += wrapped.length;
            unlinkedEvmTransactions += unlinked.length;
            sourceBytes += decodedCosmos.reduce(
                (sum, transaction) => sum + transaction.transactionBytes,
                0,
            );
            blocks.push({
                number: cosmosBlock.height,
                hash: evmBlock.hash,
                parentHash: evmBlock.parentHash,
                cosmosHash: cosmosBlock.hash,
                cosmosParentHash: cosmosBlock.parentHash,
                timestamp: hexNumber(evmBlock.timestamp),
                gasLimit: evmBlock.gasLimit,
                gasUsed: evmBlock.gasUsed,
                baseFeePerGas: evmBlock.baseFeePerGas,
                transactions: decodedCosmos,
                unlinkedEvmTransactionHashes: unlinked.map(transaction =>
                    transaction.hash.toLowerCase(),
                ),
                unlinkedEvmTransactions: unlinked,
            });
        }

        const firstBlock = blocks[0];
        const lastBlock = blocks[blocks.length - 1];
        return {
            schemaVersion: REPLAY_SCHEMA_VERSION,
            capturedAt: new Date().toISOString(),
            source: {
                network: 'pacific-1',
                evmChainId: PACIFIC_EVM_CHAIN_ID,
                cosmosChainId: PACIFIC_COSMOS_CHAIN_ID,
                evmRpcUrl: this.evmRpcUrl,
                cosmosRpcUrl: this.cosmosRpcUrl,
                firstBlock: start,
                lastBlock: end,
                blockCount: blocks.length,
                startTimestamp: firstBlock.timestamp,
                endTimestamp: lastBlock.timestamp,
                durationSeconds: Math.max(1, lastBlock.timestamp - firstBlock.timestamp + 1),
                tipLag,
            },
            continuity: {
                firstParentHash: firstBlock.parentHash,
                lastBlockHash: lastBlock.hash,
                firstCosmosParentHash: firstBlock.cosmosParentHash,
                lastCosmosBlockHash: lastBlock.cosmosHash,
            },
            totals: {
                canonicalTransactions,
                evmTransactions,
                cosmosOnlyTransactions: canonicalTransactions - wrappedEvmTransactions,
                linkedEvmTransactions,
                unresolvedEvmWrappers,
                unlinkedEvmTransactions,
                sourceBytes,
            },
            blocks,
        };
    }

    private async fetchRecipientCodeHashes(
        blocks: RpcBlock[],
        blockTag: number,
    ): Promise<Map<string, string>> {
        const addresses = [
            ...new Set(
                blocks.flatMap(block =>
                    block.transactions
                        .filter(transaction => transaction.to && transaction.input !== '0x')
                        .map(transaction => transaction.to!.toLowerCase()),
                ),
            ),
        ];
        const result = new Map<string, string>();
        for (const batch of chunk(addresses, 50)) {
            const requests = batch.map(address => ({
                jsonrpc: '2.0' as const,
                id: this.nextRpcId++,
                method: 'eth_getCode',
                params: [address, toHex(blockTag)],
            }));
            const responses = await this.rpcBatch<string>(requests);
            responses.forEach((response, index) => {
                const code = response.result;
                if (code && code !== '0x') result.set(batch[index], ethers.keccak256(code));
            });
        }
        return result;
    }

    private async attachCreationRuntime(
        transactions: ReplayEvmTransaction[],
        blockTag: number,
    ): Promise<void> {
        const creations = transactions.filter(
            transaction =>
                transaction.kind === 'contractCreation' &&
                transaction.receipt.status !== '0x0' &&
                transaction.receipt.contractAddress,
        );
        if (creations.length === 0) return;
        for (const batch of chunk(creations, 50)) {
            const responses = await this.rpcBatch<string>(
                batch.map(transaction => ({
                    jsonrpc: '2.0' as const,
                    id: this.nextRpcId++,
                    method: 'eth_getCode',
                    params: [transaction.receipt.contractAddress, toHex(blockTag)],
                })),
            );
            responses.forEach((response, index) => {
                const code = response.result;
                if (!code || code === '0x') return;
                batch[index].deployedRuntimeCodeBytes = ethers.getBytes(code).length;
                batch[index].deployedRuntimeCodeHash = ethers.keccak256(code);
            });
        }
    }

    private async attachTraces(transactions: ReplayEvmTransaction[]): Promise<void> {
        if (this.traceCaptureMode === 'off') return;
        const traceable = transactions.filter(transaction => transaction.kind !== 'transfer');
        await mapConcurrent(traceable, this.traceConcurrency, async transaction => {
            transaction.trace = await this.captureTrace(transaction.hash);
        });
    }

    private async captureTrace(hash: string): Promise<ReplayTraceSummary> {
        const errors: string[] = [];
        let calls: ReplayTraceSummary['calls'];
        let operations: ReplayTraceSummary['operations'];
        let stateDiff: ReplayTraceSummary['stateDiff'];
        try {
            const raw = await this.traceRpc(hash, {
                tracer: 'callTracer',
                tracerConfig: { onlyTopCall: false, withLog: false },
                timeout: `${Math.ceil(this.traceTimeoutMs / 1_000)}s`,
            });
            calls = normalizeCallTrace(raw, {
                maxDepth: this.traceMaxDepth,
                maxFrames: this.traceMaxFrames,
            });
        } catch (error) {
            errors.push(`callTracer: ${errorMessage(error)}`);
        }
        if (this.traceCaptureMode === 'full') {
            try {
                const raw = await this.traceRpc(hash, {
                    disableMemory: true,
                    disableStack: true,
                    disableStorage: true,
                    enableReturnData: false,
                    timeout: `${Math.ceil(this.traceTimeoutMs / 1_000)}s`,
                });
                operations = summarizeStructLogs(raw);
            } catch (error) {
                errors.push(`structLogs: ${errorMessage(error)}`);
            }
            try {
                const raw = await this.traceRpc(hash, {
                    tracer: 'prestateTracer',
                    tracerConfig: { diffMode: true },
                    timeout: `${Math.ceil(this.traceTimeoutMs / 1_000)}s`,
                });
                stateDiff = summarizePrestateDiff(raw);
            } catch (error) {
                errors.push(`prestateTracer: ${errorMessage(error)}`);
            }
        }
        const requested = this.traceCaptureMode === 'full' ? 3 : 1;
        const available =
            Number(Boolean(calls)) + Number(Boolean(operations)) + Number(Boolean(stateDiff));
        return {
            requestedMode: this.traceCaptureMode === 'full' ? 'full' : 'calls',
            availability:
                available === 0 ? 'error' : available === requested ? 'available' : 'partial',
            calls,
            operations,
            stateDiff,
            errors: errors.length > 0 ? errors : undefined,
        };
    }

    private async traceRpc(hash: string, options: Record<string, unknown>): Promise<unknown> {
        let lastError: unknown;
        for (let attempt = 0; attempt < this.traceMaxRetries; attempt++) {
            const controller = new AbortController();
            const timer = setTimeout(() => controller.abort(), this.traceTimeoutMs);
            try {
                const request: RpcRequest = {
                    jsonrpc: '2.0',
                    id: this.nextRpcId++,
                    method: 'debug_traceTransaction',
                    params: [hash, options],
                };
                const response = await fetch(this.evmRpcUrl, {
                    method: 'POST',
                    headers: { 'content-type': 'application/json' },
                    body: JSON.stringify(request),
                    signal: controller.signal,
                });
                if (!response.ok) throw new Error(`HTTP ${response.status} ${response.statusText}`);
                const body = (await response.json()) as RpcResponse<unknown>;
                if (body.error) throw new Error(`RPC ${body.error.code}: ${body.error.message}`);
                if (body.result === undefined)
                    throw new Error('debug_traceTransaction returned no result');
                return body.result;
            } catch (error) {
                lastError = error;
                if (attempt + 1 < this.traceMaxRetries) await retryDelay(attempt);
            } finally {
                clearTimeout(timer);
            }
        }
        throw lastError;
    }

    private async fetchEvmChunk(
        start: number,
        end: number,
    ): Promise<{
        blocks: RpcBlock[];
        receipts: Map<string, RpcReceipt>;
    }> {
        const blockRequests: RpcRequest[] = [];
        const receiptRequests: RpcRequest[] = [];
        for (let height = start; height <= end; height++) {
            blockRequests.push({
                jsonrpc: '2.0',
                id: this.nextRpcId++,
                method: 'eth_getBlockByNumber',
                params: [toHex(height), true],
            });
            receiptRequests.push({
                jsonrpc: '2.0',
                id: this.nextRpcId++,
                method: 'eth_getBlockReceipts',
                params: [toHex(height)],
            });
        }
        const [blockResponses, receiptResponses] = await Promise.all([
            this.rpcBatch<RpcBlock>(blockRequests),
            this.rpcBatch<RpcReceipt[]>(receiptRequests),
        ]);
        const blocks = blockResponses
            .map(response => response.result)
            .filter((block): block is RpcBlock => block !== undefined);
        const receipts = new Map<string, RpcReceipt>();
        for (const response of receiptResponses) {
            for (const receipt of response.result ?? []) {
                receipts.set(receipt.transactionHash.toLowerCase(), receipt);
            }
        }
        return { blocks, receipts };
    }

    private async fetchCosmosChunk(start: number, end: number): Promise<CosmosBlockData[]> {
        const requests: CosmosRpcRequest[] = [];
        for (let height = start; height <= end; height++) {
            requests.push({
                jsonrpc: '2.0',
                id: `block:${height}`,
                method: 'block',
                params: { height: String(height) },
            });
            requests.push({
                jsonrpc: '2.0',
                id: `results:${height}`,
                method: 'block_results',
                params: { height: String(height) },
            });
        }
        const responses = await this.cosmosRpcBatch(requests);
        const byId = new Map(responses.map(response => [response.id, response.result]));
        const blocks: CosmosBlockData[] = [];
        for (let height = start; height <= end; height++) {
            const blockResult = byId.get(`block:${height}`) as CosmosBlockResult | undefined;
            const executionResult = byId.get(`results:${height}`) as
                | CosmosBlockResultsResult
                | undefined;
            if (!blockResult?.block) throw new Error(`Cosmos block ${height} returned no block`);
            blocks.push({
                height,
                hash: blockResult.block_id.hash.toLowerCase(),
                parentHash: (blockResult.block.header.last_block_id?.hash ?? '').toLowerCase(),
                chainId: blockResult.block.header.chain_id,
                timestamp: Math.floor(Date.parse(blockResult.block.header.time) / 1_000),
                txs: blockResult.block.data.txs ?? [],
                results: executionResult?.txs_results ?? [],
            });
        }
        return blocks;
    }

    private async getBlock(height: number, fullTransactions: boolean): Promise<RpcBlock> {
        return this.rpc<RpcBlock>('eth_getBlockByNumber', [toHex(height), fullTransactions]);
    }

    private async rpc<T>(method: string, params: unknown[] = []): Promise<T> {
        const id = this.nextRpcId++;
        const [response] = await this.rpcBatch<T>([{ jsonrpc: '2.0', id, method, params }]);
        if (response.result === undefined) throw new Error(`${method} returned no result`);
        return response.result;
    }

    private async rpcBatch<T>(requests: RpcRequest[]): Promise<RpcResponse<T>[]> {
        let lastError: unknown;
        for (let attempt = 0; attempt < this.maxRetries; attempt++) {
            try {
                const response = await fetch(this.evmRpcUrl, {
                    method: 'POST',
                    headers: { 'content-type': 'application/json' },
                    body: JSON.stringify(requests),
                });
                if (!response.ok) throw new Error(`HTTP ${response.status} ${response.statusText}`);
                const body = (await response.json()) as RpcResponse<T>[] | RpcResponse<T>;
                if (!Array.isArray(body)) {
                    throw new Error(body.error?.message ?? 'EVM RPC returned a non-batch response');
                }
                const error = body.find(item => item.error);
                if (error?.error)
                    throw new Error(`RPC ${error.error.code}: ${error.error.message}`);
                return orderRpcBatchResponses(requests, body);
            } catch (error) {
                lastError = error;
                if (attempt + 1 < this.maxRetries) await retryDelay(attempt);
            }
        }
        throw lastError;
    }

    private async cosmosRpcBatch(requests: CosmosRpcRequest[]): Promise<
        Array<{
            id: string;
            result?: CosmosBlockResult | CosmosBlockResultsResult;
            error?: { message: string };
        }>
    > {
        let lastError: unknown;
        for (let attempt = 0; attempt < this.maxRetries; attempt++) {
            try {
                const response = await fetch(this.cosmosRpcUrl, {
                    method: 'POST',
                    headers: { 'content-type': 'application/json' },
                    body: JSON.stringify(requests),
                });
                if (!response.ok) {
                    throw new Error(`Cosmos RPC HTTP ${response.status} ${response.statusText}`);
                }
                const body = (await response.json()) as
                    | Array<{
                          id: string;
                          result?: CosmosBlockResult | CosmosBlockResultsResult;
                          error?: { message: string };
                      }>
                    | { error?: { message: string } };
                if (!Array.isArray(body)) {
                    throw new Error(
                        body.error?.message ?? 'Cosmos RPC returned a non-batch response',
                    );
                }
                const error = body.find(item => item.error);
                if (error?.error) throw new Error(`Cosmos RPC: ${error.error.message}`);
                return body;
            } catch (error) {
                lastError = error;
                if (attempt + 1 < this.maxRetries) await retryDelay(attempt);
            }
        }
        throw lastError;
    }
}

export function segmentFilename(start: number, end: number): string {
    return `pacific-1-${String(start).padStart(10, '0')}-${String(end).padStart(10, '0')}.json`;
}

export async function writeSegmentAtomic(
    outputDirectory: string,
    segment: ReplaySegment,
): Promise<string> {
    const output = path.join(
        outputDirectory,
        segmentFilename(segment.source.firstBlock, segment.source.lastBlock),
    );
    await writeJsonAtomic(output, segment);
    return output;
}

function decodeCosmosTransactions(block: CosmosBlockData): ReplayCosmosTransaction[] {
    if (block.txs.length !== block.results.length) {
        throw new Error(
            `Cosmos block ${block.height} has ${block.txs.length} txs but ` +
                `${block.results.length} results`,
        );
    }
    return block.txs.map((rawBase64, index) => {
        const rawBytes = Buffer.from(rawBase64, 'base64');
        const txRaw = TxRaw.decode(rawBytes);
        const body = TxBody.decode(txRaw.bodyBytes);
        const authInfo = AuthInfo.decode(txRaw.authInfoBytes);
        const result = block.results[index];
        const fee: Record<string, string> = {};
        for (const coin of authInfo.fee?.amount ?? []) fee[coin.denom] = coin.amount;
        const messages = body.messages.map(message => ({
            typeUrl: message.typeUrl,
            valueBase64: Buffer.from(message.value).toString('base64'),
        }));
        return {
            index,
            hash: crypto.createHash('sha256').update(rawBytes).digest('hex').toUpperCase(),
            rawBase64,
            transactionBytes: rawBytes.length,
            memo: body.memo,
            messages,
            fee,
            gasLimit: String(authInfo.fee?.gasLimit ?? 0n),
            result: {
                code: result.code ?? 0,
                gasWanted: result.gas_wanted ?? '0',
                gasUsed: result.gas_used ?? '0',
                eventCount: result.events?.length ?? 0,
            },
            isEvm: messages.some(message => message.typeUrl === EVM_MESSAGE_TYPE),
        };
    });
}

export function toReplayEvmTransaction(
    transaction: RpcTransaction,
    receipt: RpcReceipt,
    codeHashes: Map<string, string>,
): ReplayEvmTransaction {
    const inputBytes = Math.max(0, (transaction.input.length - 2) / 2);
    const kind = !transaction.to
        ? 'contractCreation'
        : transaction.input === '0x'
        ? 'transfer'
        : 'contractCall';
    return {
        hash: transaction.hash,
        blockNumber: hexNumber(transaction.blockNumber),
        transactionIndex: hexNumber(transaction.transactionIndex),
        from: transaction.from,
        to: transaction.to,
        nonce: hexNumber(transaction.nonce),
        chainId: transaction.chainId ?? toHex(PACIFIC_EVM_CHAIN_ID),
        type: hexNumber(transaction.type),
        kind,
        input: transaction.input,
        inputBytes,
        selector: inputBytes >= 4 ? transaction.input.slice(0, 10).toLowerCase() : null,
        value: transaction.value,
        gasLimit: transaction.gas,
        gasPrice: transaction.gasPrice,
        maxFeePerGas: transaction.maxFeePerGas,
        maxPriorityFeePerGas: transaction.maxPriorityFeePerGas,
        accessList: transaction.accessList,
        sourceSerializedBytes: serializedTransactionBytes(transaction),
        recipientCodeHash: transaction.to
            ? codeHashes.get(transaction.to.toLowerCase())
            : undefined,
        receipt: {
            transactionHash: receipt.transactionHash,
            gasUsed: receipt.gasUsed,
            effectiveGasPrice: receipt.effectiveGasPrice,
            status: receipt.status,
            contractAddress: receipt.contractAddress,
            logs: receipt.logs ?? [],
        },
    };
}

function serializedTransactionBytes(transaction: RpcTransaction): number | undefined {
    if (!transaction.r || !transaction.s || (!transaction.v && transaction.yParity === undefined)) {
        return undefined;
    }
    try {
        const parsed = ethers.Transaction.from({
            type: hexNumber(transaction.type),
            to: transaction.to,
            nonce: hexNumber(transaction.nonce),
            gasLimit: transaction.gas,
            gasPrice: transaction.gasPrice,
            maxFeePerGas: transaction.maxFeePerGas,
            maxPriorityFeePerGas: transaction.maxPriorityFeePerGas,
            data: transaction.input,
            value: transaction.value,
            chainId: transaction.chainId ?? toHex(PACIFIC_EVM_CHAIN_ID),
            accessList: transaction.accessList as ethers.AccessListish | undefined,
            signature: {
                r: transaction.r,
                s: transaction.s,
                v: transaction.v ? hexNumber(transaction.v) : 27 + hexNumber(transaction.yParity),
            },
        });
        return ethers.getBytes(parsed.serialized).length;
    } catch {
        return undefined;
    }
}

function validateBlockCoverage(
    evmBlocks: RpcBlock[],
    cosmosBlocks: CosmosBlockData[],
    start: number,
    end: number,
): void {
    const expected = end - start + 1;
    if (evmBlocks.length !== expected || cosmosBlocks.length !== expected) {
        throw new Error(
            `Incomplete segment ${start}..${end}: EVM ${evmBlocks.length}/${expected}, ` +
                `Cosmos ${cosmosBlocks.length}/${expected}`,
        );
    }
    for (let index = 0; index < expected; index++) {
        const height = start + index;
        if (
            hexNumber(evmBlocks[index].number) !== height ||
            cosmosBlocks[index].height !== height
        ) {
            throw new Error(`Non-contiguous segment at expected block ${height}`);
        }
        if (cosmosBlocks[index].chainId !== PACIFIC_COSMOS_CHAIN_ID) {
            throw new Error(
                `Refusing Cosmos chain ${cosmosBlocks[index].chainId}; expected ${PACIFIC_COSMOS_CHAIN_ID}`,
            );
        }
    }
}

function validateBlockPair(evm: RpcBlock, cosmos: CosmosBlockData, previous?: ReplayBlock): void {
    const evmTimestamp = hexNumber(evm.timestamp);
    if (Math.abs(evmTimestamp - cosmos.timestamp) > 1) {
        throw new Error(
            `Timestamp mismatch at ${cosmos.height}: EVM ${evmTimestamp}, Cosmos ${cosmos.timestamp}`,
        );
    }
    if (previous && evm.parentHash.toLowerCase() !== previous.hash.toLowerCase()) {
        throw new Error(`Broken EVM parent continuity at block ${cosmos.height}`);
    }
    if (
        previous &&
        cosmos.parentHash &&
        cosmos.parentHash.toLowerCase() !== previous.cosmosHash.toLowerCase()
    ) {
        throw new Error(`Broken Cosmos parent continuity at block ${cosmos.height}`);
    }
}

function blockRanges(
    start: number,
    end: number,
    size: number,
): Array<{ start: number; end: number }> {
    const heights = Array.from({ length: end - start + 1 }, (_, offset) => start + offset);
    return chunk(heights, size).map(range => ({
        start: range[0],
        end: range[range.length - 1],
    }));
}

function chunk<T>(values: T[], size: number): T[][] {
    const chunks: T[][] = [];
    for (let index = 0; index < values.length; index += size) {
        chunks.push(values.slice(index, index + size));
    }
    return chunks;
}

function hexNumber(value: string | undefined): number {
    return value ? Number(BigInt(value)) : 0;
}

function toHex(value: number): string {
    return `0x${value.toString(16)}`;
}

async function retryDelay(attempt: number): Promise<void> {
    const delay = Math.min(15_000, 1_000 * 2 ** attempt) + Math.floor(Math.random() * 500);
    await new Promise(resolve => setTimeout(resolve, delay));
}

function errorMessage(error: unknown): string {
    const message = error instanceof Error ? error.message : String(error);
    return message.replace(/0x[0-9a-f]{16,}/gi, '0x…').slice(0, 512);
}

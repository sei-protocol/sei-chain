import { expect } from 'chai';
import { ethers } from 'ethers';
import { Encoder } from '@sei-js/cosmos/encoding';
import { MsgSend } from 'cosmjs-types/cosmos/bank/v1beta1/tx';
import { Writer } from 'protobufjs';
import {
    loadCaptureConfig,
    loadBufferedConfig,
    loadReplayConfig,
    loadTargetConfig,
    verifyTargetCosmosRpc,
} from '../src/config';
import { correlateEvmWrapper } from '../src/replay/evmCorrelation';
import {
    asSetCodeTransaction,
    buildEvmReplay,
    encodeCallGraphHarness,
    encodeSyntheticCreationHarness,
    EvmAdapterContext,
} from '../src/replay/evmAdapters';
import {
    REPLAY_DEPLOYMENT_SCHEMA_VERSION,
    ReplayBlock,
    ReplayCosmosTransaction,
    ReplayEvmTransaction,
    ReplaySegment,
} from '../src/replay/replayTypes';
import { buildCosmosReplay } from '../src/replay/cosmosAdapters';
import { orderRpcBatchResponses } from '../src/replay/pacificSource';
import { validateReplaySegments } from '../src/replay/corpus';
import {
    normalizeCallTrace,
    summarizePrestateDiff,
    summarizeStructLogs,
} from '../src/replay/traceCapture';
import { replayEntriesForBlock } from '../src/replay/replayScheduling';
import { SUSHI_V2_PROVENANCE } from '../src/sushiV2';
import { queryEvmAssociation } from '../src/association';

const source = (overrides: Partial<ReplayEvmTransaction> = {}): ReplayEvmTransaction => ({
    hash: `0x${'11'.repeat(32)}`,
    blockNumber: 1,
    transactionIndex: 0,
    from: ethers.Wallet.createRandom().address,
    to: ethers.Wallet.createRandom().address,
    nonce: 0,
    chainId: '1329',
    type: 2,
    kind: 'contractCall',
    input: '0xdeadbeef' + '12'.repeat(80),
    inputBytes: 84,
    selector: '0xdeadbeef',
    value: '0',
    gasLimit: '200000',
    maxFeePerGas: '2',
    maxPriorityFeePerGas: '1',
    receipt: {
        transactionHash: `0x${'11'.repeat(32)}`,
        gasUsed: '100000',
        status: '0x1',
        logs: [],
    },
    ...overrides,
});

const context = (): EvmAdapterContext => ({
    chainId: 1328n,
    deployment: {
        schemaVersion: REPLAY_DEPLOYMENT_SCHEMA_VERSION,
        network: 'atlantic-2',
        chainId: 1328,
        sushiV2: SUSHI_V2_PROVENANCE,
        contracts: {
            profileHarness: ethers.Wallet.createRandom().address,
            callGraphHarness: ethers.Wallet.createRandom().address,
            syntheticCreationHarness: ethers.Wallet.createRandom().address,
            tokenA: ethers.Wallet.createRandom().address,
            tokenB: ethers.Wallet.createRandom().address,
            router: ethers.Wallet.createRandom().address,
            dexOutputTokenProxy: ethers.Wallet.createRandom().address,
            v3Router: ethers.Wallet.createRandom().address,
            masterChef: ethers.Wallet.createRandom().address,
            lendingPoolProxy: ethers.Wallet.createRandom().address,
            liquidStakingProxy: ethers.Wallet.createRandom().address,
            strategyVaultProxy: ethers.Wallet.createRandom().address,
        },
    },
    users: [
        {
            index: 1,
            derivationPath: "m/44'/118'/0'/0/1",
            seiAddress: 'sei1one',
            evmAddress: ethers.Wallet.createRandom().address,
        },
        {
            index: 2,
            derivationPath: "m/44'/118'/0'/0/2",
            seiAddress: 'sei1two',
            evmAddress: ethers.Wallet.createRandom().address,
        },
    ],
    workerIndex: 0,
    sequence: 7,
    runSalt: 1,
    nonce: 3,
    fees: { gasPrice: 1n, maxFeePerGas: 2n, maxPriorityFeePerGas: 1n },
    maxGasPerTransaction: 5_000_000n,
    maxValueWei: 1_000_000n,
    maxCalldataBytes: 1024,
});

const cosmosSource = (
    messages: ReplayCosmosTransaction['messages'],
    overrides: Partial<ReplayCosmosTransaction> = {},
): ReplayCosmosTransaction => ({
    index: 0,
    hash: 'cosmos-hash',
    rawBase64: '',
    transactionBytes: 200,
    memo: '',
    messages,
    fee: { usei: '25000' },
    gasLimit: '200000',
    result: { code: 0, gasWanted: '200000', gasUsed: '100000', eventCount: 1 },
    isEvm: false,
    ...overrides,
});

describe('load generator pure behavior', () => {
    it('parses centralized defaults and environment overrides', () => {
        const capture = loadCaptureConfig({
            RECORD_MINUTES: '30',
            RPC_CONCURRENCY: '4',
            REPLAY_DIR: 'runtime/custom',
        });
        expect(capture.recordMinutes).to.equal(30);
        expect(capture.evmConcurrency).to.equal(4);
        expect(capture.traceCaptureMode).to.equal('calls');
        expect(capture.traceConcurrency).to.equal(1);
        expect(capture.replayDirectory).to.equal(
            `${process.cwd()}/runtime/custom`,
        );
        expect(loadCaptureConfig({}).replayDirectory).to.equal(
            `${process.cwd()}/runtime/replay/pacific-1/pacific-1-20m`,
        );
        expect(loadReplayConfig({ EXECUTE: '0' }).execute).to.equal(false);
        expect(loadReplayConfig({ EXECUTE: '1' }).execute).to.equal(true);
        expect(loadReplayConfig({}).evmReceiptTimeoutMs).to.equal(60_000);
        expect(loadReplayConfig({ EVM_RECEIPT_TIMEOUT_MS: '2500' }).evmReceiptTimeoutMs).to.equal(
            2_500,
        );
        expect(loadReplayConfig({ REPLAY_FROM_START: '1' }).replayFromStart).to.equal(
            true,
        );
        expect(loadBufferedConfig({}).startMode).to.equal('latest');
        expect(loadBufferedConfig({}).initialBufferBlocks).to.equal(200);
        expect(loadBufferedConfig({}).runDurationSeconds).to.equal(7_200);
        expect(loadBufferedConfig({ RUN_DURATION_HOURS: '0.5' }).runDurationSeconds).to.equal(
            1_800,
        );
        expect(
            loadBufferedConfig({
                RUN_DURATION_HOURS: '2',
                RUN_DURATION_SECONDS: '90',
            }).runDurationSeconds,
        ).to.equal(90);
        expect(loadBufferedConfig({}).cleanupConsumedSegments).to.equal(true);
        expect(loadReplayConfig({}).replayDirectory).to.equal(
            `${process.cwd()}/runtime/replay/pacific-1/pacific-1-20m`,
        );
        expect(loadReplayConfig({}).cleanupConsumedSegments).to.equal(false);
        expect(loadReplayConfig({}).metricsHost).to.equal('127.0.0.1');
        expect(
            loadReplayConfig({ CLEANUP_CONSUMED_SEGMENTS: '1' }).cleanupConsumedSegments,
        ).to.equal(true);
        expect(loadReplayConfig({}).fixturePrepareGasLimit).to.equal(2_000_000n);
        expect(
            loadReplayConfig({ FIXTURE_PREPARE_GAS_LIMIT: '3000000' })
                .fixturePrepareGasLimit,
        ).to.equal(3_000_000n);
        expect(
            loadTargetConfig({ TARGET_NETWORK: 'arctic-1', USER_COUNT: '5' }).usersPath,
        ).to.equal(`${process.cwd()}/runtime/replay-users/arctic-1-5.json`);
        expect(loadTargetConfig({ TARGET_NETWORK: 'arctic-1' }).deploymentPath).to.equal(
            `${process.cwd()}/runtime/replay-deployments/arctic-1-v4.json`,
        );
    });

    it('rejects unsafe or unknown target configuration', () => {
        expect(() => loadTargetConfig({ TARGET_NETWORK: 'pacific-1' })).to.throw(
            'TARGET_NETWORK',
        );
        expect(() => loadReplayConfig({ MAX_TPS: '0' })).to.throw('MAX_TPS');
        expect(() => loadCaptureConfig({ BLOCKS_PER_BATCH: '21' })).to.throw(
            'BLOCKS_PER_BATCH',
        );
        expect(() => loadBufferedConfig({ BUFFER_START_MODE: 'oldest' })).to.throw(
            'BUFFER_START_MODE',
        );
        expect(() => loadReplayConfig({ CLEANUP_CONSUMED_SEGMENTS: 'yes' })).to.throw(
            'CLEANUP_CONSUMED_SEGMENTS',
        );
        expect(() => loadCaptureConfig({ TRACE_CAPTURE_MODE: 'raw' })).to.throw(
            'TRACE_CAPTURE_MODE',
        );
        expect(() => loadBufferedConfig({ TRACE_MAX_FRAMES: '257' })).to.throw(
            'TRACE_MAX_FRAMES',
        );
        expect(() => loadCaptureConfig({ START_BLOCK: '0' })).to.throw('START_BLOCK');
        expect(() => loadCaptureConfig({ START_BLOCK: ' ' })).to.throw('START_BLOCK');
    });

    it('rejects a mismatched Cosmos target chain', async () => {
        const target = loadTargetConfig({ TARGET_NETWORK: 'arctic-1' });
        await verifyTargetCosmosRpc(target, { getChainId: async () => 'arctic-1' });
        try {
            await verifyTargetCosmosRpc(target, { getChainId: async () => 'pacific-1' });
            expect.fail('expected Cosmos chain mismatch');
        } catch (error) {
            expect(String(error)).to.contain('expected arctic-1');
        }
    });

    it('orders RPC batch responses by request id and rejects omissions', () => {
        expect(
            orderRpcBatchResponses(
                [{ id: 2 }, { id: 1 }],
                [
                    { id: 1, result: 'one' },
                    { id: 2, result: 'two' },
                ],
            ).map(response => response.result),
        ).to.deep.equal(['two', 'one']);
        expect(() =>
            orderRpcBatchResponses(
                [{ id: 1 }, { id: 2 }],
                [{ id: 1, result: 'one' }],
            ),
        ).to.throw('omitted batch id 2');
    });

    it('builds bounded Cosmos bank traffic and handles privileged messages', () => {
        const users = context().users;
        const encoded = MsgSend.encode({
            fromAddress: 'sei1source',
            toAddress: 'sei1destination',
            amount: [{ denom: 'usei', amount: '5000' }],
        }).finish();
        const bank = buildCosmosReplay(
            cosmosSource([
                {
                    typeUrl: '/cosmos.bank.v1beta1.MsgSend',
                    valueBase64: Buffer.from(encoded).toString('base64'),
                },
            ]),
            { users, workerIndex: 0, maxMessages: 10 },
        );
        expect(bank.adapter).to.equal('cosmosBankSend');
        expect((bank.messages?.[0].value as MsgSend).amount[0].amount).to.equal('1000');

        const privileged = cosmosSource([
            { typeUrl: '/seiprotocol.seichain.oracle.MsgVote', valueBase64: '' },
        ]);
        expect(
            buildCosmosReplay(privileged, {
                users,
                workerIndex: 0,
                maxMessages: 10,
                privilegedMode: 'skip',
            }).fidelity,
        ).to.equal('skipped');
        expect(
            buildCosmosReplay(privileged, {
                users,
                workerIndex: 0,
                maxMessages: 10,
                privilegedMode: 'shape',
            }).adapter,
        ).to.equal('cosmosPrivilegedShape');
        expect(
            buildCosmosReplay(cosmosSource([], { isEvm: true }), {
                users,
                workerIndex: 0,
                maxMessages: 10,
            }).reason,
        ).to.equal('Wrapped EVM transaction has no linked EVM entry (ante-failed)');
    });

    it('queries associations through the supported Cosmos ABCI service', async () => {
        const originalFetch = globalThis.fetch;
        const evmAddress = ethers.Wallet.createRandom().address;
        const encoded = Writer.create()
            .uint32(10)
            .string(evmAddress)
            .uint32(16)
            .bool(true)
            .finish();
        globalThis.fetch = async (_input, init) => {
            const request = JSON.parse(String(init?.body)) as {
                method: string;
                params: { path: string };
            };
            expect(request.method).to.equal('abci_query');
            expect(request.params.path).to.equal(
                '/seiprotocol.seichain.evm.Query/EVMAddressBySeiAddress',
            );
            return new Response(
                JSON.stringify({
                    result: {
                        response: {
                            code: 0,
                            value: Buffer.from(encoded).toString('base64'),
                        },
                    },
                }),
                { status: 200 },
            );
        };
        try {
            expect(
                await queryEvmAssociation('https://rpc.example', 'sei1example'),
            ).to.deep.equal({ associated: true, evmAddress });
        } finally {
            globalThis.fetch = originalFetch;
        }
    });

    it('treats an empty successful association response as unassociated', async () => {
        const originalFetch = globalThis.fetch;
        globalThis.fetch = async () =>
            new Response(JSON.stringify({ result: { response: { code: 0 } } }), {
                status: 200,
            });
        try {
            expect(
                await queryEvmAssociation('https://rpc.example', 'sei1example'),
            ).to.deep.equal({ associated: false, evmAddress: '' });
        } finally {
            globalThis.fetch = originalFetch;
        }
    });

    it('keeps malformed EVM wrappers unresolved instead of guessing', () => {
        const result = correlateEvmWrapper(Uint8Array.from([255, 255, 255]));
        expect(result.method).to.equal('unresolved');
        expect(result.hash).to.equal(undefined);
    });

    it('reconstructs a wrapped signed EVM transaction hash', async () => {
        const wallet = ethers.Wallet.createRandom();
        const signed = await wallet.signTransaction({
            chainId: 1329,
            nonce: 4,
            gasPrice: 10n,
            gasLimit: 21_000n,
            to: ethers.Wallet.createRandom().address,
            value: 7n,
            type: 0,
        });
        const transaction = ethers.Transaction.from(signed);
        const signature = transaction.signature!;
        const legacyV = 1329n * 2n + 35n + BigInt(signature.yParity);
        const legacy = Encoder.eth.LegacyTx.fromPartial({
            nonce: transaction.nonce,
            gas_price: transaction.gasPrice!.toString(),
            gas_limit: Number(transaction.gasLimit),
            to: transaction.to!,
            value: transaction.value.toString(),
            data: ethers.getBytes(transaction.data),
            v: ethers.toBeArray(legacyV),
            r: ethers.getBytes(signature.r),
            s: ethers.getBytes(signature.s),
        });
        const wrapper = Encoder.evm.MsgEVMTransaction.fromPartial({
            data: {
                type_url: `/${Encoder.eth.LegacyTx.$type}`,
                value: Encoder.eth.LegacyTx.encode(legacy).finish(),
            },
        });
        const result = correlateEvmWrapper(
            Encoder.evm.MsgEVMTransaction.encode(wrapper).finish(),
        );
        expect(result.method).to.equal('signed_payload');
        expect(result.hash).to.equal(ethers.keccak256(signed));
    });

    it('reconstructs a wrapped EIP-7702 transaction hash', async () => {
        const wallet = ethers.Wallet.createRandom();
        const authorization = await wallet.authorize({
            address: ethers.Wallet.createRandom().address,
            chainId: 1329,
            nonce: 0,
        });
        const signed = await wallet.signTransaction({
            type: 4,
            chainId: 1329,
            nonce: 0,
            maxPriorityFeePerGas: 1n,
            maxFeePerGas: 10n,
            gasLimit: 100_000n,
            to: wallet.address,
            value: 0,
            data: '0x',
            accessList: [],
            authorizationList: [authorization],
        });
        const transaction = ethers.Transaction.from(signed);
        const encoded = encodeSetCodeTx(transaction);
        const wrapper = Encoder.evm.MsgEVMTransaction.fromPartial({
            data: {
                type_url: '/seiprotocol.seichain.eth.SetCodeTx',
                value: encoded,
            },
        });
        const result = correlateEvmWrapper(
            Encoder.evm.MsgEVMTransaction.encode(wrapper).finish(),
        );
        expect(result.method).to.equal('signed_payload');
        expect(result.hash).to.equal(ethers.keccak256(signed));
    });

    it('preserves EIP-7702 envelope pressure with a target authorization', async () => {
        const wallet = ethers.Wallet.createRandom();
        const authorization = await wallet.authorize({
            address: ethers.Wallet.createRandom().address,
            chainId: 713715,
            nonce: 1,
        });
        const transaction = asSetCodeTransaction(
            {
                type: 2,
                chainId: 713715,
                nonce: 0,
                to: wallet.address,
                gasLimit: 100_000,
                maxFeePerGas: 10,
                maxPriorityFeePerGas: 1,
            },
            authorization,
        );
        expect(transaction.type).to.equal(4);
        expect(transaction.gasPrice).to.equal(undefined);
        expect(transaction.authorizationList).to.deep.equal([authorization]);
    });

    it('routes unknown EVM calls through the bounded shape harness', () => {
        const ctx = context();
        const built = buildEvmReplay(source(), ctx);
        expect(built.adapter).to.equal('profileHarness');
        expect(built.fidelity).to.equal('shape');
        expect(built.transaction?.to).to.equal(ctx.deployment.contracts.profileHarness);
        expect(ethers.getBytes(built.transaction?.data ?? '0x')).to.have.length(84);
        expect(String(built.transaction?.data).slice(0, 10)).to.equal('0xdeadbeef');
    });

    const semanticSource = (selector: string, inputBytes = 300): ReplayEvmTransaction =>
        source({
            selector,
            input: `${selector}${'00'.repeat(inputBytes - 4)}`,
            inputBytes,
        });

    const expectSemanticRoute = (
        selector: string,
        adapter: string,
        target: keyof EvmAdapterContext['deployment']['contracts'],
    ): void => {
        const ctx = context();
        const built = buildEvmReplay(semanticSource(selector), ctx);
        expect(built.adapter).to.equal(adapter);
        expect(built.fidelity).to.equal('semantic');
        expect(built.transaction?.to).to.equal(ctx.deployment.contracts[target]);
        expect(ethers.getBytes(built.transaction?.data ?? '0x')).to.have.length(300);
        expect(built.producedCalldataBytes).to.be.at.most(ctx.maxCalldataBytes);
    };

    it('routes V3 exactInputSingle through the callback router', () => {
        expectSemanticRoute('0x04e45aaf', 'uniswapV3ExactInputSingle', 'v3Router');
    });

    it('routes MasterChef deposit and withdraw', () => {
        expectSemanticRoute('0xe2bbb158', 'masterChefDeposit', 'masterChef');
        expectSemanticRoute('0x441a3e70', 'masterChefWithdraw', 'masterChef');
    });

    it('bounds the MasterChef amount argument rather than the pool id', () => {
        const farm = new ethers.Interface(['function deposit(uint256 pid,uint256 amount)']);
        const input = farm.encodeFunctionData('deposit', [3n, ethers.parseEther('5')]);
        const built = buildEvmReplay(
            source({
                selector: '0xe2bbb158',
                input,
                inputBytes: ethers.getBytes(input).length,
            }),
            context(),
        );
        const decoded = farm.decodeFunctionData('deposit', built.transaction!.data!);
        expect(decoded[0]).to.equal(0n);
        expect(decoded[1]).to.equal(ethers.parseEther('0.01'));
    });

    it('routes Aave and cToken selectors to lending', () => {
        expectSemanticRoute('0x617ba037', 'aaveShapedSupply', 'lendingPoolProxy');
        expectSemanticRoute('0x69328dec', 'aaveShapedWithdraw', 'lendingPoolProxy');
        expectSemanticRoute('0xa0712d68', 'cTokenShapedMint', 'lendingPoolProxy');
        expectSemanticRoute('0xdb006a75', 'cTokenShapedRedeem', 'lendingPoolProxy');
        expectSemanticRoute('0xc5ebeaec', 'cTokenShapedBorrow', 'lendingPoolProxy');
        expectSemanticRoute('0x0e752702', 'cTokenShapedRepayBorrow', 'lendingPoolProxy');
    });

    it('routes liquid stake and withdrawal requests', () => {
        expectSemanticRoute('0xa694fc3a', 'liquidStakingStake', 'liquidStakingProxy');
        expectSemanticRoute(
            '0x9ee679e8',
            'liquidStakingRequestWithdrawal',
            'liquidStakingProxy',
        );
        const ctx = context();
        ctx.sequence = 8;
        const deposit = buildEvmReplay(semanticSource('0x6e553f65'), ctx);
        expect(deposit.adapter).to.equal('liquidStakingErc4626Deposit');
        expect(deposit.transaction?.to).to.equal(ctx.deployment.contracts.liquidStakingProxy);
    });

    it('routes ERC4626 deposit, withdraw, and redeem to the strategy vault', () => {
        expectSemanticRoute('0x6e553f65', 'strategyVaultDeposit', 'strategyVaultProxy');
        expectSemanticRoute('0xb460af94', 'strategyVaultWithdraw', 'strategyVaultProxy');
        expectSemanticRoute('0xba087652', 'strategyVaultRedeem', 'strategyVaultProxy');

        const ctx = context();
        const strategyHash = `0x${'00'.repeat(31)}02`;
        ctx.sequence = 2;
        ctx.deployment.codeHashes = {
            strategyVaultProxy: strategyHash,
            liquidStakingProxy: `0x${'00'.repeat(31)}03`,
        };
        const exact = buildEvmReplay(
            source({
                ...semanticSource('0x6e553f65'),
                recipientCodeHash: strategyHash,
            }),
            ctx,
        );
        expect(exact.adapter).to.equal('strategyVaultDeposit');
    });

    it('normalizes nested call traces and deterministically truncates them', () => {
        const raw = {
            type: 'CALL',
            input: '0xaaaaaaaa',
            gas: '0x100',
            calls: [
                {
                    type: 'DELEGATECALL',
                    input: '0xbbbbbbbb1234',
                    gasUsed: '0x20',
                    calls: [{ type: 'STATICCALL', input: '0x', value: '0x0' }],
                },
                { type: 'CALL', input: '0xcccccccc', error: 'reverted' },
            ],
        };
        const full = normalizeCallTrace(raw, { maxDepth: 8, maxFrames: 8 });
        expect(full.frames.map(frame => [frame.type, frame.depth, frame.parent])).to.deep.equal([
            ['CALL', 0, null],
            ['DELEGATECALL', 1, 0],
            ['STATICCALL', 2, 1],
            ['CALL', 1, 0],
        ]);
        expect(full.frames[1].selector).to.equal('0xbbbbbbbb');
        expect(full.sourceFrameCount).to.equal(4);
        const truncated = normalizeCallTrace(raw, { maxDepth: 8, maxFrames: 2 });
        expect(truncated.frames.map(frame => frame.type)).to.deep.equal(['CALL', 'DELEGATECALL']);
        expect(truncated.truncated).to.equal(true);
        // Truncation bounds the retained frames but not the source frame count.
        expect(truncated.sourceFrameCount).to.equal(4);
    });

    it('counts deeply truncated trace subtrees without recursive overflow', () => {
        let raw: { type: string; calls?: unknown[] } = { type: 'CALL' };
        for (let i = 0; i < 2_000; i++) raw = { type: 'CALL', calls: [raw] };
        const normalized = normalizeCallTrace(raw, { maxDepth: 8, maxFrames: 64 });
        expect(normalized.frames).to.have.length(9);
        expect(normalized.sourceFrameCount).to.equal(2_001);
        expect(normalized.truncated).to.equal(true);
    });

    it('summarizes opcode and prestate diff profiles without values', () => {
        const operations = summarizeStructLogs({
            structLogs: [
                { op: 'SLOAD', stack: ['secret'] },
                { op: 'SSTORE' },
                { op: 'DELEGATECALL' },
                { op: 'CREATE2' },
                { op: 'LOG3' },
                { op: 'SHA3' },
            ],
        });
        expect(operations).to.include({
            sload: 1,
            sstore: 1,
            delegatecall: 1,
            create2: 1,
            logs: 1,
            keccak256: 1,
        });
        const state = summarizePrestateDiff({
            pre: { '0x1': { balance: '1', storage: { '0xaa': 'old' } } },
            post: {
                '0x1': { balance: '2', storage: { '0xaa': 'new', '0xbb': 'new' } },
                '0x2': { code: '0x60' },
            },
        });
        expect(state).to.deep.equal({
            changedAccounts: 2,
            changedStorageSlots: 2,
            code: 1,
            balance: 1,
            nonce: 0,
        });
    });

    it('routes traced unknown calls through encoded call graph harness', () => {
        const ctx = context();
        const traced = source({
            input: `0xdeadbeef${'12'.repeat(496)}`,
            inputBytes: 500,
            trace: {
                requestedMode: 'full',
                availability: 'available',
                calls: {
                    truncated: false,
                    sourceFrameCount: 2,
                    frames: [
                        {
                            index: 0,
                            parent: null,
                            depth: 0,
                            type: 'CALL',
                            selector: '0xdeadbeef',
                            inputBytes: 84,
                            valueNonZero: false,
                            gasUsed: '1000',
                            reverted: false,
                        },
                        {
                            index: 1,
                            parent: 0,
                            depth: 1,
                            type: 'DELEGATECALL',
                            selector: null,
                            inputBytes: 0,
                            valueNonZero: false,
                            gasUsed: '500',
                            reverted: false,
                        },
                    ],
                },
                operations: {
                    steps: 10,
                    sload: 2,
                    sstore: 3,
                    call: 1,
                    staticcall: 0,
                    delegatecall: 1,
                    create: 0,
                    create2: 1,
                    logs: 1,
                    log0: 0,
                    log1: 1,
                    log2: 0,
                    log3: 0,
                    log4: 0,
                    keccak256: 2,
                },
                stateDiff: {
                    changedAccounts: 1,
                    changedStorageSlots: 4,
                    code: 0,
                    balance: 0,
                    nonce: 0,
                },
            },
        });
        const built = buildEvmReplay(traced, ctx);
        expect(built.adapter).to.equal('callGraphHarness');
        expect(built.fidelity).to.equal('trace-shape');
        expect(built.transaction?.to).to.equal(ctx.deployment.contracts.callGraphHarness);
        expect(ethers.getBytes(built.transaction?.data ?? '0x')).to.have.length(500);
        const encoded = encodeCallGraphHarness(traced, 7);
        const decoded = new ethers.Interface([
            'function execute(bytes spec,uint256 salt)',
        ]).decodeFunctionData('execute', encoded);
        const spec = ethers.getBytes(decoded[0]);
        expect(spec).to.have.length(32);
        expect(spec[16]).to.equal(2);
        expect(spec[17]).to.equal(1);
    });

    it('falls back when a captured call graph exceeds harness bounds', () => {
        const frame = (index: number, depth: number) => ({
            index,
            parent: index === 0 ? null : 0,
            depth,
            type: 'CALL' as const,
            selector: null,
            inputBytes: 0,
            valueNonZero: false,
            reverted: false,
        });
        const traced = (frames: ReturnType<typeof frame>[]) =>
            source({
                trace: {
                    requestedMode: 'calls',
                    availability: 'available',
                    calls: { frames, truncated: false, sourceFrameCount: frames.length },
                },
            });

        expect(
            buildEvmReplay(
                traced([frame(0, 0), ...Array.from({ length: 64 }, (_, i) => frame(i + 1, 1))]),
                context(),
            ).adapter,
        ).to.equal('profileHarness');
        expect(
            buildEvmReplay(
                traced(Array.from({ length: 10 }, (_, depth) => frame(depth, depth))),
                context(),
            ).adapter,
        ).to.equal('profileHarness');
    });

    it('routes successful contract creations through the synthetic creation harness', () => {
        const ctx = context();
        const creation = source({
            to: null,
            kind: 'contractCreation',
            input: `0x${'60'.repeat(600)}`,
            inputBytes: 600,
            selector: '0x60606060',
            deployedRuntimeCodeBytes: 320,
            creationMethod: 'CREATE2',
            receipt: {
                transactionHash: `0x${'11'.repeat(32)}`,
                gasUsed: '500000',
                status: '0x1',
                contractAddress: ethers.Wallet.createRandom().address,
                logs: [],
            },
            trace: {
                requestedMode: 'full',
                availability: 'available',
                calls: {
                    frames: [{
                        index: 0,
                        parent: null,
                        depth: 0,
                        type: 'CREATE',
                        selector: null,
                        inputBytes: 600,
                        valueNonZero: false,
                        gasUsed: '400000',
                        reverted: false,
                    }],
                    truncated: false,
                    sourceFrameCount: 1,
                },
                operations: {
                    steps: 100,
                    sload: 0,
                    sstore: 4,
                    call: 0,
                    staticcall: 0,
                    delegatecall: 0,
                    create: 0,
                    create2: 1,
                    logs: 0,
                    log0: 0,
                    log1: 0,
                    log2: 0,
                    log3: 0,
                    log4: 0,
                    keccak256: 3,
                },
            },
        });

        const built = buildEvmReplay(creation, ctx);
        expect(built.adapter).to.equal('syntheticCreationHarness');
        expect(built.fidelity).to.equal('creation-shape');
        expect(built.transaction?.to).to.equal(
            ctx.deployment.contracts.syntheticCreationHarness,
        );
        expect(ethers.getBytes(built.transaction?.data ?? '0x')).to.have.length(600);
        const creationHarness = new ethers.Interface([
            'function deploy(uint16 runtimeBytes,uint16 stores,uint32 gasBurn,uint32 requestedInitcodeBytes,bool useCreate2,bytes32 salt)',
        ]);
        const first = creationHarness.decodeFunctionData(
            'deploy',
            encodeSyntheticCreationHarness(creation, 7, 1),
        );
        const second = creationHarness.decodeFunctionData(
            'deploy',
            encodeSyntheticCreationHarness(creation, 7, 2),
        );
        expect(first.salt).not.to.equal(second.salt);
    });

    it('includes unlinked EVM transactions exactly once in replay scheduling', () => {
        const linked = source({ hash: '0xlinked', transactionIndex: 2 });
        const unlinked = source({ hash: '0xunlinked', transactionIndex: 1 });
        const cosmos = {
            index: 0,
            hash: 'cosmos',
            rawBase64: '',
            transactionBytes: 1,
            memo: '',
            messages: [],
            fee: {},
            gasLimit: '1',
            result: { code: 0, gasWanted: '1', gasUsed: '1', eventCount: 0 },
            isEvm: true,
            evm: linked,
        };
        const block = {
            number: 1,
            hash: 'h',
            parentHash: 'p',
            cosmosHash: 'c',
            cosmosParentHash: 'cp',
            timestamp: 1,
            gasLimit: '1',
            gasUsed: '1',
            transactions: [cosmos],
            unlinkedEvmTransactions: [unlinked],
        } satisfies ReplayBlock;
        const entries = replayEntriesForBlock(block);
        expect(entries.map(entry => entry.evm?.hash)).to.deep.equal(['0xunlinked', '0xlinked']);
        expect(new Set(entries.map(entry => entry.evm?.hash)).size).to.equal(2);
    });

    it('translates native transfers to replay users', () => {
        const ctx = context();
        const built = buildEvmReplay(
            source({ kind: 'transfer', input: '0x', inputBytes: 0, selector: null, value: '99' }),
            ctx,
        );
        expect(built.adapter).to.equal('nativeTransfer');
        expect(built.transaction?.to).to.equal(ctx.users[1].evmAddress);
        expect(built.transaction?.value).to.equal(99n);
    });

    it('validates EVM and Cosmos continuity between segments', () => {
        const segment = (
            firstBlock: number,
            firstParentHash: string,
            firstCosmosParentHash: string,
        ): ReplaySegment =>
            ({
                schemaVersion: 1,
                capturedAt: '2026-01-01T00:00:00.000Z',
                source: {
                    network: 'pacific-1',
                    evmChainId: 1329,
                    cosmosChainId: 'pacific-1',
                    evmRpcUrl: 'https://evm.example',
                    cosmosRpcUrl: 'https://cosmos.example',
                    firstBlock,
                    lastBlock: firstBlock,
                    blockCount: 0,
                    startTimestamp: 0,
                    endTimestamp: 0,
                    durationSeconds: 0,
                    tipLag: 10,
                },
                continuity: {
                    firstParentHash,
                    lastBlockHash: `evm-${firstBlock}`,
                    firstCosmosParentHash,
                    lastCosmosBlockHash: `cosmos-${firstBlock}`,
                },
                totals: {
                    canonicalTransactions: 0,
                    evmTransactions: 0,
                    cosmosOnlyTransactions: 0,
                    sourceBytes: 0,
                },
                blocks: [],
            }) satisfies ReplaySegment;
        const first = segment(1, 'evm-0', 'cosmos-0');
        const second = segment(2, 'evm-1', 'wrong-cosmos-parent');
        expect(() => validateReplaySegments([first, second])).to.throw(
            'Cosmos continuity mismatch',
        );
    });
});

function encodeSetCodeTx(transaction: ethers.Transaction): Uint8Array {
    const signature = transaction.signature!;
    const writer = Writer.create();
    writeSdkInt(writer, 1, transaction.chainId);
    writer.uint32(16).uint64(transaction.nonce);
    writeSdkInt(writer, 3, transaction.maxPriorityFeePerGas!);
    writeSdkInt(writer, 4, transaction.maxFeePerGas!);
    writer.uint32(40).uint64(transaction.gasLimit.toString());
    writer.uint32(50).string(transaction.to!);
    writeSdkInt(writer, 7, transaction.value);
    writer.uint32(66).bytes(ethers.getBytes(transaction.data));
    for (const authorization of transaction.authorizationList ?? []) {
        const nested = writer.uint32(82).fork();
        writeSdkInt(nested, 1, authorization.chainId);
        nested.uint32(18).string(authorization.address);
        nested.uint32(24).uint64(authorization.nonce.toString());
        nested.uint32(34).bytes(ethers.toBeArray(authorization.signature.yParity));
        nested.uint32(42).bytes(ethers.getBytes(authorization.signature.r));
        nested.uint32(50).bytes(ethers.getBytes(authorization.signature.s));
        nested.ldelim();
    }
    writer.uint32(90).bytes(ethers.toBeArray(signature.yParity));
    writer.uint32(98).bytes(ethers.getBytes(signature.r));
    writer.uint32(106).bytes(ethers.getBytes(signature.s));
    return writer.finish();
}

function writeSdkInt(writer: Writer, field: number, value: bigint): void {
    writer.uint32((field << 3) | 2).bytes(Buffer.from(value.toString()));
}

import { ethers } from 'ethers';
import { maxBigInt, minBigInt } from '../numeric';
import {
    ReplayCallFrame,
    ReplayDeploymentManifest,
    ReplayEvmTransaction,
    ReplayUserManifest,
} from './replayTypes';

export type ReplayFidelity =
    | 'semantic'
    | 'trace-shape'
    | 'creation-shape'
    | 'shape'
    | 'skipped';

export interface ReplayFeeState {
    gasPrice: bigint;
    maxFeePerGas: bigint;
    maxPriorityFeePerGas: bigint;
}

export interface EvmAdapterContext {
    chainId: bigint;
    deployment: ReplayDeploymentManifest;
    users: ReplayUserManifest['users'];
    workerIndex: number;
    sequence: number;
    runSalt: number;
    nonce: number;
    fees: ReplayFeeState;
    maxGasPerTransaction: bigint;
    maxValueWei: bigint;
    maxCalldataBytes: number;
}

export interface BuiltEvmReplay {
    adapter: string;
    fidelity: ReplayFidelity;
    transaction?: ethers.TransactionRequest;
    sourceCalldataBytes: number;
    producedCalldataBytes: number;
    reason?: string;
}

export function asSetCodeTransaction(
    transaction: ethers.TransactionRequest,
    authorization: ethers.Authorization,
): ethers.TransactionRequest {
    return {
        ...transaction,
        type: 4,
        gasPrice: undefined,
        accessList: transaction.accessList ?? [],
        authorizationList: [authorization],
    };
}

const ERC20 = new ethers.Interface([
    'function transfer(address,uint256) returns(bool)',
    'function approve(address,uint256) returns(bool)',
    'function mint(address,uint256)',
]);
const ROUTER = new ethers.Interface([
    'function swapExactTokensForTokens(uint256,uint256,address[],address,uint256)',
]);
const V3_ROUTER = new ethers.Interface([
    'function exactInputSingle((address tokenIn,address tokenOut,uint24 fee,address recipient,uint256 amountIn,uint256 amountOutMinimum,uint160 sqrtPriceLimitX96)) returns(uint256)',
]);
const FARM = new ethers.Interface([
    'function deposit(uint256 pid,uint256 amount)',
    'function withdraw(uint256 pid,uint256 amount)',
]);
const LENDING = new ethers.Interface([
    'function supply(address asset,uint256 amount,address onBehalfOf,uint16 referralCode)',
    'function withdraw(address asset,uint256 amount,address to) returns(uint256)',
    'function mint(uint256 amount) returns(uint256)',
    'function redeem(uint256 redeemTokens) returns(uint256)',
    'function borrow(uint256 amount)',
    'function repayBorrow(uint256 amount) returns(uint256)',
]);
const LIQUID_STAKING = new ethers.Interface([
    'function stake(uint256 assets) returns(uint256)',
    'function deposit(uint256 assets,address receiver) returns(uint256)',
    'function requestWithdrawal(uint256 shares) returns(uint256)',
]);
const VAULT = new ethers.Interface([
    'function deposit(uint256 assets,address receiver) returns(uint256)',
    'function withdraw(uint256 assets,address receiver,address owner) returns(uint256)',
    'function redeem(uint256 shares,address receiver,address owner) returns(uint256)',
]);
const NFT = new ethers.Interface(['function safeMint(address,uint256)']);
const HARNESS = new ethers.Interface([
    'function run(uint256 targetExecutionGas,uint256 stateWrites,uint256 salt,bytes payload)',
]);
const CALL_GRAPH_HARNESS = new ethers.Interface([
    'function execute(bytes spec,uint256 salt)',
]);
const CALL_GRAPH_MAX_DEPTH = 8;
const CALL_GRAPH_MAX_FRAMES = 64;
const SYNTHETIC_CREATION_HARNESS = new ethers.Interface([
    'function deploy(uint16 runtimeBytes,uint16 stores,uint32 gasBurn,uint32 requestedInitcodeBytes,bool useCreate2,bytes32 salt) returns(address)',
]);

const SELECTORS = {
    transfer: ERC20.getFunction('transfer')!.selector.toLowerCase(),
    approve: ERC20.getFunction('approve')!.selector.toLowerCase(),
    mint: ERC20.getFunction('mint')!.selector.toLowerCase(),
    swapExactTokensForTokens: ROUTER.getFunction('swapExactTokensForTokens')!.selector.toLowerCase(),
    safeMint: NFT.getFunction('safeMint')!.selector.toLowerCase(),
    harnessRun: HARNESS.getFunction('run')!.selector.toLowerCase(),
    exactInputSingle: '0x04e45aaf',
    farmDeposit: '0xe2bbb158',
    farmWithdraw: '0x441a3e70',
    lendingSupply: '0x617ba037',
    lendingWithdraw: LENDING.getFunction('withdraw')!.selector.toLowerCase(),
    lendingMint: '0xa0712d68',
    lendingRedeem: '0xdb006a75',
    lendingBorrow: '0xc5ebeaec',
    lendingRepayBorrow: LENDING.getFunction('repayBorrow')!.selector.toLowerCase(),
    liquidStake: '0xa694fc3a',
    erc4626Deposit: '0x6e553f65',
    liquidRequestWithdrawal: '0x9ee679e8',
    erc4626Withdraw: '0xb460af94',
    erc4626Redeem: '0xba087652',
};

export function buildEvmReplay(
    source: ReplayEvmTransaction,
    context: EvmAdapterContext,
): BuiltEvmReplay {
    if (source.type === 3) {
        return skipped(source, 'EIP-4844 blob transactions are not supported on Sei');
    }
    const worker = context.users[context.workerIndex % context.users.length];
    const recipient = context.users[(context.workerIndex + 1) % context.users.length];
    const contracts = context.deployment.contracts;

    if (source.kind === 'transfer') {
        return built(
            source,
            'nativeTransfer',
            'semantic',
            envelope(source, context, {
                to: recipient.evmAddress,
                data: '0x',
                value: boundedValue(source.value, context.maxValueWei),
                gasLimit: 30_000n,
            }),
        );
    }

    if (source.selector === SELECTORS.transfer && contracts.tokenA && contracts.tokenB) {
        const [, sourceAmount] = safeDecode(ERC20, 'transfer', source.input) ?? [
            ethers.ZeroAddress,
            1n,
        ];
        const data = fitCalldata(
            ERC20.encodeFunctionData('transfer', [
                recipient.evmAddress,
                boundedTokenAmount(sourceAmount),
            ]),
            source.inputBytes,
            context.maxCalldataBytes,
            context.sequence,
        );
        return built(
            source,
            'erc20Transfer',
            'semantic',
            envelope(source, context, {
                to: context.sequence % 2 === 0 ? contracts.tokenA : contracts.tokenB,
                data,
                value: 0n,
                gasLimit: boundedGas(source, context, 120_000n),
            }),
        );
    }

    if (source.selector === SELECTORS.approve && contracts.tokenA && contracts.tokenB) {
        const decoded = safeDecode(ERC20, 'approve', source.input);
        const amount = decoded ? boundedTokenAmount(decoded[1]) : ethers.MaxUint256;
        const data = fitCalldata(
            ERC20.encodeFunctionData('approve', [contracts.router ?? recipient.evmAddress, amount]),
            source.inputBytes,
            context.maxCalldataBytes,
            context.sequence,
        );
        return built(
            source,
            'erc20Approve',
            'semantic',
            envelope(source, context, {
                to: context.sequence % 2 === 0 ? contracts.tokenA : contracts.tokenB,
                data,
                value: 0n,
                gasLimit: boundedGas(source, context, 120_000n),
            }),
        );
    }

    if (source.selector === SELECTORS.mint && contracts.tokenA && contracts.tokenB) {
        const decoded = safeDecode(ERC20, 'mint', source.input);
        const amount = decoded ? boundedTokenAmount(decoded[1]) : 1n;
        const data = fitCalldata(
            ERC20.encodeFunctionData('mint', [worker.evmAddress, amount]),
            source.inputBytes,
            context.maxCalldataBytes,
            context.sequence,
        );
        return built(
            source,
            'erc20Mint',
            'semantic',
            envelope(source, context, {
                to: context.sequence % 2 === 0 ? contracts.tokenA : contracts.tokenB,
                data,
                value: 0n,
                gasLimit: boundedGas(source, context, 150_000n),
            }),
        );
    }

    if (
        source.selector === SELECTORS.swapExactTokensForTokens &&
        contracts.router &&
        contracts.tokenA &&
        contracts.tokenB
    ) {
        const even = context.sequence % 2 === 0;
        const data = fitCalldata(
            ROUTER.encodeFunctionData('swapExactTokensForTokens', [
                ethers.parseEther('0.001'),
                0,
                even
                    ? [contracts.tokenA, contracts.tokenB]
                    : [contracts.tokenB, contracts.tokenA],
                worker.evmAddress,
                Math.floor(Date.now() / 1_000) + 600,
            ]),
            source.inputBytes,
            context.maxCalldataBytes,
            context.sequence,
        );
        return built(
            source,
            'uniswapV2Swap',
            'semantic',
            envelope(source, context, {
                to: contracts.router,
                data,
                value: 0n,
                gasLimit: boundedGas(source, context, 400_000n),
            }),
        );
    }

    if (
        source.selector === SELECTORS.exactInputSingle &&
        contracts.v3Router &&
        contracts.tokenA &&
        contracts.dexOutputTokenProxy
    ) {
        const replay = semanticProtocolCall(
            source,
            context,
            'uniswapV3ExactInputSingle',
            contracts.v3Router,
            V3_ROUTER.encodeFunctionData('exactInputSingle', [
                {
                    tokenIn: contracts.tokenA,
                    tokenOut: contracts.dexOutputTokenProxy,
                    fee: 3000,
                    recipient: worker.evmAddress,
                    amountIn: syntheticDepositAmount(source, V3_ROUTER, 'exactInputSingle'),
                    amountOutMinimum: 0,
                    sqrtPriceLimitX96: 0,
                },
            ]),
            700_000n,
        );
        if (replay) return replay;
    }

    if (
        (source.selector === SELECTORS.farmDeposit ||
            source.selector === SELECTORS.farmWithdraw) &&
        contracts.masterChef
    ) {
        const depositing = source.selector === SELECTORS.farmDeposit;
        const method = depositing ? 'deposit' : 'withdraw';
        const amount = depositing
            ? syntheticDepositAmount(source, FARM, method)
            : 1n;
        const replay = semanticProtocolCall(
            source,
            context,
            depositing ? 'masterChefDeposit' : 'masterChefWithdraw',
            contracts.masterChef,
            FARM.encodeFunctionData(method, [0, amount]),
            500_000n,
        );
        if (replay) return replay;
    }

    if (contracts.lendingPoolProxy && contracts.tokenA) {
        let method: string | undefined;
        let adapter: string | undefined;
        let args: unknown[] = [];
        switch (source.selector) {
            case SELECTORS.lendingSupply:
                method = 'supply';
                adapter = 'aaveShapedSupply';
                args = [
                    contracts.tokenA,
                    syntheticDepositAmount(source, LENDING, method),
                    worker.evmAddress,
                    0,
                ];
                break;
            case SELECTORS.lendingWithdraw:
                method = 'withdraw';
                adapter = 'aaveShapedWithdraw';
                args = [contracts.tokenA, 1n, worker.evmAddress];
                break;
            case SELECTORS.lendingMint:
                method = 'mint';
                adapter = 'cTokenShapedMint';
                args = [syntheticDepositAmount(source, LENDING, method)];
                break;
            case SELECTORS.lendingRedeem:
                method = 'redeem';
                adapter = 'cTokenShapedRedeem';
                args = [1n];
                break;
            case SELECTORS.lendingBorrow:
                method = 'borrow';
                adapter = 'cTokenShapedBorrow';
                args = [1n];
                break;
            case SELECTORS.lendingRepayBorrow:
                method = 'repayBorrow';
                adapter = 'cTokenShapedRepayBorrow';
                args = [1n];
                break;
        }
        if (method && adapter) {
            const replay = semanticProtocolCall(
                source,
                context,
                adapter,
                contracts.lendingPoolProxy,
                LENDING.encodeFunctionData(method, args),
                650_000n,
            );
            if (replay) return replay;
        }
    }

    if (
        source.selector === SELECTORS.liquidStake &&
        contracts.liquidStakingProxy
    ) {
        const replay = semanticProtocolCall(
            source,
            context,
            'liquidStakingStake',
            contracts.liquidStakingProxy,
            LIQUID_STAKING.encodeFunctionData('stake', [
                syntheticDepositAmount(source, LIQUID_STAKING, 'stake'),
            ]),
            550_000n,
        );
        if (replay) return replay;
    }

    if (
        source.selector === SELECTORS.erc4626Deposit &&
        contracts.liquidStakingProxy &&
        contracts.strategyVaultProxy
    ) {
        // 0x6e553f65 is shared by ERC4626-style protocols. A captured recipient
        // code hash deterministically partitions ambiguous sources: exact fixture
        // hashes win, then hash parity, then sequence parity.
        const liquidHash = context.deployment.codeHashes?.liquidStakingProxy;
        const strategyHash = context.deployment.codeHashes?.strategyVaultProxy;
        const matchesLiquid =
            source.recipientCodeHash !== undefined &&
            liquidHash !== undefined &&
            source.recipientCodeHash.toLowerCase() === liquidHash.toLowerCase();
        const matchesStrategy =
            source.recipientCodeHash !== undefined &&
            strategyHash !== undefined &&
            source.recipientCodeHash.toLowerCase() === strategyHash.toLowerCase();
        const hashChoosesLiquid =
            source.recipientCodeHash !== undefined &&
            BigInt(source.recipientCodeHash) % 2n === 0n;
        const useLiquid =
            matchesLiquid ||
            (!matchesStrategy &&
                (hashChoosesLiquid ||
                    (source.recipientCodeHash === undefined && context.sequence % 2 === 0)));
        const target = useLiquid
            ? contracts.liquidStakingProxy
            : contracts.strategyVaultProxy;
        const abi = useLiquid ? LIQUID_STAKING : VAULT;
        const replay = semanticProtocolCall(
            source,
            context,
            useLiquid ? 'liquidStakingErc4626Deposit' : 'strategyVaultDeposit',
            target,
            abi.encodeFunctionData('deposit', [
                syntheticDepositAmount(source, abi, 'deposit'),
                worker.evmAddress,
            ]),
            600_000n,
        );
        if (replay) return replay;
    }

    if (
        source.selector === SELECTORS.liquidRequestWithdrawal &&
        contracts.liquidStakingProxy
    ) {
        const replay = semanticProtocolCall(
            source,
            context,
            'liquidStakingRequestWithdrawal',
            contracts.liquidStakingProxy,
            LIQUID_STAKING.encodeFunctionData('requestWithdrawal', [1n]),
            500_000n,
        );
        if (replay) return replay;
    }

    if (
        (source.selector === SELECTORS.erc4626Withdraw ||
            source.selector === SELECTORS.erc4626Redeem) &&
        contracts.strategyVaultProxy
    ) {
        const withdrawing = source.selector === SELECTORS.erc4626Withdraw;
        const method = withdrawing ? 'withdraw' : 'redeem';
        const replay = semanticProtocolCall(
            source,
            context,
            withdrawing ? 'strategyVaultWithdraw' : 'strategyVaultRedeem',
            contracts.strategyVaultProxy,
            VAULT.encodeFunctionData(method, [1n, worker.evmAddress, worker.evmAddress]),
            700_000n,
        );
        if (replay) return replay;
    }

    if (source.selector === SELECTORS.safeMint && contracts.nft) {
        const data = fitCalldata(
            NFT.encodeFunctionData('safeMint', [
                recipient.evmAddress,
                BigInt(Date.now()) * 1_000_000n + BigInt(context.sequence),
            ]),
            source.inputBytes,
            context.maxCalldataBytes,
            context.sequence,
        );
        return built(
            source,
            'erc721SafeMint',
            'semantic',
            envelope(source, context, {
                to: contracts.nft,
                data,
                value: 0n,
                gasLimit: boundedGas(source, context, 600_000n),
            }),
        );
    }

    if (
        source.kind === 'contractCreation' &&
        source.receipt.contractAddress &&
        source.receipt.status !== '0x0' &&
        contracts.syntheticCreationHarness
    ) {
        const naturalData = encodeSyntheticCreationHarness(
            source,
            context.sequence,
            context.runSalt,
        );
        if (ethers.getBytes(naturalData).length <= context.maxCalldataBytes) {
            return built(
                source,
                'syntheticCreationHarness',
                'creation-shape',
                envelope(source, context, {
                    to: contracts.syntheticCreationHarness,
                    data: fitCalldata(
                        naturalData,
                        source.inputBytes,
                        context.maxCalldataBytes,
                        context.sequence,
                    ),
                    value: boundedValue(source.value, context.maxValueWei),
                    gasLimit: boundedGas(source, context, 1_000_000n),
                }),
            );
        }
    }

    if (
        source.trace?.calls?.frames.length &&
        contracts.callGraphHarness &&
        isExecutableCallGraph(source.trace.calls.frames)
    ) {
        const naturalData = encodeCallGraphHarness(source, context.sequence);
        if (ethers.getBytes(naturalData).length <= context.maxCalldataBytes) {
            // Solidity's ABI decoder permits trailing calldata. Preserve the
            // source byte pressure when the bounded call-tree spec is smaller.
            const data = fitCalldata(
                naturalData,
                source.inputBytes,
                context.maxCalldataBytes,
                context.sequence,
            );
            return built(
                source,
                'callGraphHarness',
                'trace-shape',
                envelope(source, context, {
                    to: contracts.callGraphHarness,
                    data,
                    value: boundedValue(source.value, context.maxValueWei),
                    gasLimit: boundedGas(source, context, 300_000n),
                }),
            );
        }
    }

    if (!contracts.profileHarness) {
        return skipped(source, 'No semantic adapter and deployment has no ProfileLoadHarness');
    }
    const targetBytes = Math.min(source.inputBytes, context.maxCalldataBytes);
    const gasUsed = BigInt(source.receipt.gasUsed);
    const intrinsic = 21_000n + BigInt(targetBytes * 16);
    const targetExecutionGas = gasUsed > intrinsic ? gasUsed - intrinsic : 0n;
    const data = shapeFallbackCalldata(
        source,
        targetBytes,
        targetExecutionGas,
        source.receipt.logs.length > 0 ? 1n : 0n,
        context.sequence,
    );
    return built(
        source,
        'profileHarness',
        'shape',
        envelope(source, context, {
            to: contracts.profileHarness,
            data,
            value: boundedValue(source.value, context.maxValueWei),
            gasLimit: boundedGas(source, context, targetExecutionGas + 150_000n),
        }),
    );
}

export function encodeCallGraphHarness(source: ReplayEvmTransaction, seed: number): string {
    const frames = source.trace?.calls?.frames ?? [];
    if (!isExecutableCallGraph(frames)) {
        throw new Error('Call graph exceeds harness bounds or has invalid depth ordering');
    }
    const operation = source.trace?.operations;
    const state = source.trace?.stateDiff;
    const bytes = new Uint8Array(frames.length * 16);
    const staticDepths: number[] = [];
    for (const [index, frame] of frames.entries()) {
        while (staticDepths.length > 0 && staticDepths.at(-1)! >= frame.depth) staticDepths.pop();
        const inStaticContext = staticDepths.length > 0 || frame.type === 'STATICCALL';
        if (frame.type === 'STATICCALL') staticDepths.push(frame.depth);
        const offset = index * 16;
        bytes[offset] =
            frame.type === 'STATICCALL'
                ? 1
                : frame.type === 'DELEGATECALL'
                  ? 2
                  : frame.type === 'CREATE'
                    ? 3
                    : frame.type === 'CREATE2'
                      ? 4
                      : 0;
        bytes[offset + 1] = frame.depth;
        const root = index === 0;
        putU16(bytes, offset + 2, root ? Math.min(16, operation?.sload ?? 0) : 0);
        putU16(
            bytes,
            offset + 4,
            root && !inStaticContext
                ? Math.min(16, Math.max(operation?.sstore ?? 0, state?.changedStorageSlots ?? 0))
                : 0,
        );
        putU16(bytes, offset + 6, root && !inStaticContext ? Math.min(16, operation?.logs ?? 0) : 0);
        putU16(bytes, offset + 8, root ? Math.min(16, operation?.keccak256 ?? 0) : 0);
        putU32(bytes, offset + 10, Math.min(100_000, Number(frame.gasUsed ?? 0)));
        putU16(
            bytes,
            offset + 14,
            root ? Math.min(16, (operation?.create ?? 0) + (operation?.create2 ?? 0)) : 0,
        );
    }
    return CALL_GRAPH_HARNESS.encodeFunctionData('execute', [bytes, seed]);
}

function isExecutableCallGraph(frames: ReplayCallFrame[]): boolean {
    if (frames.length === 0 || frames.length > CALL_GRAPH_MAX_FRAMES) return false;
    return frames.every(
        (frame, index) =>
            Number.isInteger(frame.depth) &&
            frame.depth >= 0 &&
            frame.depth <= CALL_GRAPH_MAX_DEPTH &&
            (index === 0
                ? frame.depth === 0
                : frame.depth <= frames[index - 1].depth + 1),
    );
}

export function encodeSyntheticCreationHarness(
    source: ReplayEvmTransaction,
    seed: number,
    runSalt = 0,
): string {
    const runtimeBytes = Math.min(
        24_576,
        Math.max(1, source.deployedRuntimeCodeBytes ?? Math.floor(source.inputBytes / 2)),
    );
    const stores = Math.min(
        16,
        Math.max(
            source.trace?.operations?.sstore ?? 0,
            source.trace?.stateDiff?.changedStorageSlots ?? 0,
        ),
    );
    const intrinsic = 21_000n + BigInt(source.inputBytes * 16);
    const observed = BigInt(source.receipt.gasUsed);
    const gasBurn = Number(minBigInt(100_000n, observed > intrinsic ? observed - intrinsic : 0n));
    const initcodeBytes = Math.min(49_152, Math.max(16, source.inputBytes));
    const useCreate2 = source.creationMethod === 'CREATE2';
    return SYNTHETIC_CREATION_HARNESS.encodeFunctionData('deploy', [
        runtimeBytes,
        stores,
        gasBurn,
        initcodeBytes,
        useCreate2,
        ethers.solidityPackedKeccak256(['uint256', 'uint256'], [runSalt, seed]),
    ]);
}

function putU16(target: Uint8Array, offset: number, value: number): void {
    target[offset] = (value >>> 8) & 0xff;
    target[offset + 1] = value & 0xff;
}

function putU32(target: Uint8Array, offset: number, value: number): void {
    target[offset] = (value >>> 24) & 0xff;
    target[offset + 1] = (value >>> 16) & 0xff;
    target[offset + 2] = (value >>> 8) & 0xff;
    target[offset + 3] = value & 0xff;
}

function envelope(
    source: ReplayEvmTransaction,
    context: EvmAdapterContext,
    values: {
        to?: string;
        data: string;
        value: bigint;
        gasLimit: bigint;
    },
): ethers.TransactionRequest {
    const common = {
        chainId: context.chainId,
        nonce: context.nonce,
        to: values.to,
        data: values.data,
        value: values.value,
        gasLimit: values.gasLimit,
    };
    if (source.type === 0) {
        return { ...common, type: 0, gasPrice: context.fees.gasPrice };
    }
    if (source.type === 1) {
        return { ...common, type: 1, gasPrice: context.fees.gasPrice, accessList: [] };
    }
    return {
        ...common,
        type: 2,
        maxFeePerGas: context.fees.maxFeePerGas,
        maxPriorityFeePerGas: context.fees.maxPriorityFeePerGas,
    };
}

export function fitCalldata(
    natural: string,
    targetBytes: number,
    maximumBytes: number,
    seed: number,
): string {
    const naturalBytes = ethers.getBytes(natural);
    if (naturalBytes.length > maximumBytes) {
        throw new Error(
            `Natural semantic calldata ${naturalBytes.length} exceeds MAX_CALLDATA_BYTES=${maximumBytes}`,
        );
    }
    const boundedTarget = Math.min(targetBytes, maximumBytes);
    if (boundedTarget <= naturalBytes.length) return natural;
    return ethers.hexlify(
        ethers.concat([
            naturalBytes,
            deterministicBytes(boundedTarget - naturalBytes.length, seed),
        ]),
    );
}

function semanticProtocolCall(
    source: ReplayEvmTransaction,
    context: EvmAdapterContext,
    adapter: string,
    to: string,
    naturalData: string,
    minimumGas: bigint,
): BuiltEvmReplay | undefined {
    if (ethers.getBytes(naturalData).length > context.maxCalldataBytes) return undefined;
    return built(
        source,
        adapter,
        'semantic',
        envelope(source, context, {
            to,
            data: fitCalldata(
                naturalData,
                source.inputBytes,
                context.maxCalldataBytes,
                context.sequence,
            ),
            value: 0n,
            gasLimit: boundedGas(source, context, minimumGas),
        }),
    );
}

function syntheticDepositAmount(
    source: ReplayEvmTransaction,
    abi: ethers.Interface,
    method: string,
): bigint {
    const decoded = safeDecode(abi, method, source.input);
    const candidate =
        decoded && method === 'exactInputSingle'
            ? decoded[0]?.amountIn
            : decoded?.[
                  method === 'supply' || source.selector === SELECTORS.farmDeposit ? 1 : 0
              ];
    return maxBigInt(1n, minBigInt(toBigInt(candidate), ethers.parseEther('0.01')));
}

function toBigInt(value: unknown): bigint {
    try {
        return BigInt(value as bigint);
    } catch {
        return ethers.parseEther('0.001');
    }
}

function shapeFallbackCalldata(
    source: ReplayEvmTransaction,
    targetBytes: number,
    targetExecutionGas: bigint,
    stateWrites: bigint,
    seed: number,
): string {
    if (targetBytes === 0) return '0x';
    if (targetBytes < 68) {
        const original = ethers.getBytes(source.input);
        const output = new Uint8Array(targetBytes);
        output.set(original.slice(0, targetBytes));
        if (targetBytes >= 4) {
            const selector = ethers.hexlify(output.slice(0, 4)).toLowerCase();
            if (selector === SELECTORS.harnessRun) output[3] ^= 1;
        }
        return ethers.hexlify(output);
    }
    const selector =
        source.selector && source.selector !== SELECTORS.harnessRun
            ? ethers.getBytes(source.selector)
            : deterministicBytes(4, seed);
    const controls = ethers.concat([
        selector,
        ethers.zeroPadValue(ethers.toBeHex(targetExecutionGas), 32),
        ethers.zeroPadValue(ethers.toBeHex(stateWrites), 32),
    ]);
    return ethers.hexlify(
        ethers.concat([controls, deterministicBytes(targetBytes - 68, seed)]),
    );
}

function deterministicBytes(length: number, seed: number): Uint8Array {
    if (length <= 0) return new Uint8Array();
    const digest = ethers.getBytes(ethers.id(`pacific-replay-${seed}`));
    const output = new Uint8Array(length);
    for (let index = 0; index < length; index++) output[index] = digest[index % digest.length];
    return output;
}

function boundedGas(
    source: ReplayEvmTransaction,
    context: EvmAdapterContext,
    minimum: bigint,
): bigint {
    const observed = BigInt(source.receipt.gasUsed);
    const buffered = observed + observed / 2n + 50_000n;
    return minBigInt(context.maxGasPerTransaction, maxBigInt(minimum, buffered));
}

function boundedValue(raw: string, maximum: bigint): bigint {
    return minBigInt(BigInt(raw), maximum);
}

function boundedTokenAmount(value: unknown): bigint {
    let parsed: bigint;
    try {
        parsed = BigInt(value as bigint);
    } catch {
        parsed = 1n;
    }
    return maxBigInt(1n, minBigInt(parsed, ethers.parseEther('100')));
}

function safeDecode(
    abi: ethers.Interface,
    method: string,
    data: string,
): ethers.Result | undefined {
    try {
        return abi.decodeFunctionData(method, data);
    } catch {
        return undefined;
    }
}

function built(
    source: ReplayEvmTransaction,
    adapter: string,
    fidelity: ReplayFidelity,
    transaction: ethers.TransactionRequest,
): BuiltEvmReplay {
    return {
        adapter,
        fidelity,
        transaction,
        sourceCalldataBytes: source.inputBytes,
        producedCalldataBytes: ethers.getBytes(transaction.data ?? '0x').length,
    };
}

function skipped(source: ReplayEvmTransaction, reason: string): BuiltEvmReplay {
    return {
        adapter: 'skipped',
        fidelity: 'skipped',
        sourceCalldataBytes: source.inputBytes,
        producedCalldataBytes: 0,
        reason,
    };
}

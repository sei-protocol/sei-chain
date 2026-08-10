import { coins } from '@cosmjs/amino';
import { EncodeObject } from '@cosmjs/proto-signing';
import { StdFee } from '@cosmjs/stargate';
import { MsgSend } from 'cosmjs-types/cosmos/bank/v1beta1/tx';
import { maxBigInt, minBigInt } from '../numeric';
import { ReplayCosmosTransaction, ReplayUserManifest } from './replayTypes';
import { ReplayFidelity } from './evmAdapters';

export interface CosmosAdapterContext {
    users: ReplayUserManifest['users'];
    workerIndex: number;
    maxMessages: number;
    privilegedMode?: 'skip' | 'shape';
}

export interface BuiltCosmosReplay {
    adapter: string;
    fidelity: ReplayFidelity;
    messages?: EncodeObject[];
    fee?: StdFee;
    memoPrefix?: string;
    targetTransactionBytes: number;
    reason?: string;
}

const BANK_SEND = '/cosmos.bank.v1beta1.MsgSend';
const PRIVILEGED_PATTERNS = [
    '.oracle.',
    '.gov.',
    '.slashing.',
    '.upgrade.',
    'MsgAggregateExchangeRateVote',
    'MsgDelegateFeedConsent',
];

export function buildCosmosReplay(
    source: ReplayCosmosTransaction,
    context: CosmosAdapterContext,
): BuiltCosmosReplay {
    if (source.isEvm) {
        return skipped(
            source,
            source.evm
                ? 'Wrapped EVM transaction is replayed through the EVM lane'
                : 'Wrapped EVM transaction has no linked EVM entry (ante-failed)',
        );
    }
    const types = source.messages.map(message => message.typeUrl);
    const isPrivileged = types.some(type =>
        PRIVILEGED_PATTERNS.some(pattern => type.includes(pattern)),
    );
    if (isPrivileged && context.privilegedMode !== 'shape') {
        return skipped(source, `Privileged/system message: ${types.join(', ')}`);
    }
    const worker = context.users[context.workerIndex % context.users.length];
    const limited = source.messages.slice(0, context.maxMessages);
    const allBank = limited.length > 0 && limited.every(message => message.typeUrl === BANK_SEND);
    if (allBank) {
        const messages = limited.map((message, offset) => {
            let amount = 1n;
            try {
                const decoded = MsgSend.decode(Buffer.from(message.valueBase64, 'base64'));
                const sourceUsei = decoded.amount.find(coin => coin.denom === 'usei');
                if (sourceUsei) amount = boundedAmount(BigInt(sourceUsei.amount));
            } catch {
                // A malformed source message falls back to a one-usei send.
            }
            const recipient =
                context.users[(context.workerIndex + offset + 1) % context.users.length];
            return {
                typeUrl: BANK_SEND,
                value: {
                    fromAddress: worker.seiAddress,
                    toAddress: recipient.seiAddress,
                    amount: coins(amount.toString(), 'usei'),
                },
            };
        });
        return {
            adapter: 'cosmosBankSend',
            fidelity: 'semantic',
            messages,
            fee: targetFee(source, messages.length),
            memoPrefix: 'Pacific replay bank',
            targetTransactionBytes: source.transactionBytes,
        };
    }

    // A controlled bank workload preserves message cardinality and transaction
    // byte pressure without pretending to reproduce unsupported module state.
    const count = Math.max(1, limited.length);
    const messages: EncodeObject[] = Array.from({ length: count }, (_, offset) => {
        const recipient = context.users[(context.workerIndex + offset + 1) % context.users.length];
        return {
            typeUrl: BANK_SEND,
            value: {
                fromAddress: worker.seiAddress,
                toAddress: recipient.seiAddress,
                amount: coins('1', 'usei'),
            },
        };
    });
    return {
        adapter: isPrivileged ? 'cosmosPrivilegedShape' : 'cosmosBankShape',
        fidelity: 'shape',
        messages,
        fee: targetFee(source, messages.length),
        memoPrefix: `Pacific ${isPrivileged ? 'privileged ' : ''}shape ${types
            .join('+')
            .slice(0, 80)}`,
        targetTransactionBytes: source.transactionBytes,
    };
}

function targetFee(source: ReplayCosmosTransaction, messageCount: number): StdFee {
    const sourceGas = BigInt(source.gasLimit);
    const minimumGas = BigInt(200_000 + Math.max(0, messageCount - 1) * 100_000);
    const gas = minBigInt(5_000_000n, maxBigInt(sourceGas, minimumGas));
    const sourceUsei = source.fee.usei ? BigInt(source.fee.usei) : 0n;
    const amount = minBigInt(
        1_000_000n,
        maxBigInt(sourceUsei, BigInt(21_000 + messageCount * 5_000)),
    );
    return { amount: coins(amount.toString(), 'usei'), gas: gas.toString() };
}

function skipped(source: ReplayCosmosTransaction, reason: string): BuiltCosmosReplay {
    return {
        adapter: 'skipped',
        fidelity: 'skipped',
        targetTransactionBytes: source.transactionBytes,
        reason,
    };
}

function boundedAmount(value: bigint): bigint {
    return minBigInt(1_000n, maxBigInt(1n, value));
}

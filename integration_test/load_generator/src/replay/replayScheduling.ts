import { ReplayBlock, ReplayCosmosTransaction, ReplayEvmTransaction } from './replayTypes';

export interface ReplaySchedulingEntry {
    cosmos?: ReplayCosmosTransaction;
    evm?: ReplayEvmTransaction;
    sourceCosmosHash?: string;
    orderingKey: number;
}

/**
 * EVM indexes and CometBFT indexes are independent. Linked wrappers provide
 * anchors; Cosmos-only entries are interpolated in Cosmos order and unlinked
 * EVM entries retain EVM transaction-index order.
 */
export function replayEntriesForBlock(block: ReplayBlock): ReplaySchedulingEntry[] {
    const unlinked = block.unlinkedEvmTransactions ?? [];
    const maxEvmIndex = Math.max(
        0,
        ...block.transactions.flatMap(transaction =>
            transaction.evm ? [transaction.evm.transactionIndex] : [],
        ),
        ...unlinked.map(transaction => transaction.transactionIndex),
    );
    const denominator = Math.max(1, block.transactions.length);
    const entries: Array<ReplaySchedulingEntry & { stable: number }> = block.transactions.map(
        (cosmos, index) => ({
            cosmos,
            evm: cosmos.isEvm ? cosmos.evm : undefined,
            sourceCosmosHash: cosmos.hash,
            orderingKey:
                cosmos.isEvm && cosmos.evm
                    ? cosmos.evm.transactionIndex
                    : ((index + 0.5) * (maxEvmIndex + 1)) / denominator,
            stable: index * 2,
        }),
    );
    for (const evm of unlinked) {
        entries.push({
            evm,
            orderingKey: evm.transactionIndex,
            stable: evm.transactionIndex * 2 + 1,
        });
    }
    return entries
        .sort((left, right) => left.orderingKey - right.orderingKey || left.stable - right.stable)
        .map(({ stable: _stable, ...entry }) => entry);
}

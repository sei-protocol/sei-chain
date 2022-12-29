# Oracle MidBlock Spec

## Context

Currently, the oracle module processes oracle pricing votes from validators via transactions, and executes them as part of the normal DeliverTx. Then, the oracle module calculates the oracle asset pricing in End Block once it has processed all oracle vote transactions for the block. This has the behavior where new oracle prices are stored as part of EndBlock, and so aren’t used by transactions or contracts until the following block.

When working with a voting period of 1 we can make further optimizations to improve the freshness of oracle asset pricing. We would like to have oracle votes finalized BEFORE the execution of non-oracle vote txs, such that the newest oracle asset pricing is finalized WITHIN the block so that transactions and contracts are utilizing the freshest oracle prices possible.

## Design

Currently, transaction execution is performed as part of ProcessTxs, which is performed after BeginBlock and before EndBlock. The approach to improve oracle data freshness is to first filter the transactions into oracle votes and non-oracle votes. Then, we initially begin ProcessTxs specifically for the oracle votes. Then, we introduce a new stage called MidBlock, which modules can implement in order to perform some logic after ProcessOracleTxs and before other transactions.

Practically, this is a fairly straightforward solution because we can use the same DeliverTx logic to execute the two groups of transactions, and would only need to partition the transactions appropriately. Additionally, because we can use the existing DeliverTx logic, we get parallelization for the oracle transactions out of the box, which is a slight performance improvement since all oracle votes can be executed in parallel (although practically not significantly different performance due to low oracle vote volume).

With this approach, an oracle vote for block N would be processed separately from other TXs in block N, and the MidBlock function that separates the two can be used to calculate oracle consensus based on validators votes. This way, any transaction in block N would utilize the oracle price updated in the block N midblock IFF the oracle vote had enough total voting power.

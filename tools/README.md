# Sei Tools
This page provides an overview of a couple of built-in tools provided by seid command line. 

## TX-Scanner 
TX-Scanner is a tool that helps to scan transactions that are missing or failed 
to be indexed. This is usually used on archive nodes where all historical transactions
need to be persisted and queryable. 

In the current COSMOS SDK, there's a known bug: during shutdown, transactions for the 
current block might not be correctly indexed. The consequence of not indexing transactions properly 
is that those transactions can't be queried, even though they exist in the block data.

This tool helps to scan archive nodes to find out all the missing transactions so that
later on you can reindex all the missing transactions to make them queryable again.

### Usage
It is recommended to run this tool as a background daemon process:
```
# Run in the background
seid tools scan-tx --start-height 1 --state-dir ./ > scan.log &
```
The tool will keep scanning from the start height, if there's already
a state file exist in `state-dir`, it will instead start from the previous height.

The tool won't stop until you manually stop it, once it hit latest
block height, it will keep running and waiting for new blocks to come,
it keep scanning all the newly produced blocks once they are committed.

### State Format
A typical state file (`tx-scanner-state.json`) would look like this:
```
{
    "last_processed_height": 44394319,
    "blocks_missing_txs": [123400, 2124542]
}
```
last_processed_height: int64, represent last processed block height
blocks_missing_txs: []int64, represent all the block heights that is missing transactions

### ReIndex Transactions
Once you finish scanning and found some missing transactions, you can
use the tendermint cli tool to reindex these blocks. You need to stop
the seid process before running the below command:
```
seid tendermint reindex-event --start-height 2124542 --end-height 2124543
```

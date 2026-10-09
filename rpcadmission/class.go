// Package rpcadmission classifies RPC methods and bounds their concurrent resource use.
package rpcadmission

import "strings"

// MethodClass groups RPC methods with similar execution and memory costs.
type MethodClass string

const (
	ClassCheapRead              MethodClass = "cheap_read"
	ClassNormalRead             MethodClass = "normal_read"
	ClassSearchIndex            MethodClass = "search_index"
	ClassBlockTxMaterialization MethodClass = "block_tx_materialization"
	ClassLogQuery               MethodClass = "log_query"
	ClassEVMExecution           MethodClass = "evm_execution"
	ClassTrace                  MethodClass = "trace"
	ClassBroadcast              MethodClass = "broadcast"
	ClassSubscription           MethodClass = "subscription"
)

var allMethodClasses = []MethodClass{
	ClassCheapRead,
	ClassNormalRead,
	ClassSearchIndex,
	ClassBlockTxMaterialization,
	ClassLogQuery,
	ClassEVMExecution,
	ClassTrace,
	ClassBroadcast,
	ClassSubscription,
}

var exactMethodClasses = map[string]MethodClass{
	// EVM JSON-RPC reads whose work is constant or bounded to small metadata.
	"eth_accounts":             ClassCheapRead,
	"eth_blobBaseFee":          ClassCheapRead,
	"eth_blockNumber":          ClassCheapRead,
	"eth_chainId":              ClassCheapRead,
	"eth_coinbase":             ClassCheapRead,
	"eth_gasPrice":             ClassCheapRead,
	"eth_maxPriorityFeePerGas": ClassCheapRead,
	"eth_protocolVersion":      ClassCheapRead,
	"eth_syncing":              ClassCheapRead,
	"net_listening":            ClassCheapRead,
	"net_peerCount":            ClassCheapRead,
	"net_version":              ClassCheapRead,
	"web3_clientVersion":       ClassCheapRead,
	"web3_sha3":                ClassCheapRead,

	// EVM index lookups.
	"eth_getTransactionByBlockHashAndIndex":   ClassSearchIndex,
	"eth_getTransactionByBlockNumberAndIndex": ClassSearchIndex,
	"eth_getTransactionByHash":                ClassSearchIndex,
	"eth_getTransactionErrorByHash":           ClassSearchIndex,
	"eth_getTransactionReceipt":               ClassSearchIndex,
	"eth_getVMError":                          ClassSearchIndex,
	"sei_getCosmosTx":                         ClassSearchIndex,

	// EVM methods that construct whole blocks, transactions, or receipt sets.
	"eth_getBlockByHash":                   ClassBlockTxMaterialization,
	"eth_getBlockByNumber":                 ClassBlockTxMaterialization,
	"eth_getBlockReceipts":                 ClassBlockTxMaterialization,
	"eth_getBlockTransactionCountByHash":   ClassBlockTxMaterialization,
	"eth_getBlockTransactionCountByNumber": ClassBlockTxMaterialization,
	"eth_getUncleByBlockHashAndIndex":      ClassBlockTxMaterialization,
	"eth_getUncleByBlockNumberAndIndex":    ClassBlockTxMaterialization,
	"eth_feeHistory":                       ClassBlockTxMaterialization,
	"debug_getRawBlock":                    ClassBlockTxMaterialization,
	"debug_getRawReceipts":                 ClassBlockTxMaterialization,
	"debug_getRawTransaction":              ClassSearchIndex,
	"txpool_content":                       ClassBlockTxMaterialization,

	// Log/filter reads can scan many blocks and materialize large result sets.
	"eth_getFilterChanges":            ClassLogQuery,
	"eth_getFilterLogs":               ClassLogQuery,
	"eth_getLogs":                     ClassLogQuery,
	"eth_newBlockFilter":              ClassLogQuery,
	"eth_newFilter":                   ClassLogQuery,
	"eth_newPendingTransactionFilter": ClassLogQuery,
	"eth_uninstallFilter":             ClassLogQuery,

	// Methods that execute the EVM without committing a transaction.
	"eth_call":                  ClassEVMExecution,
	"eth_createAccessList":      ClassEVMExecution,
	"eth_estimateGas":           ClassEVMExecution,
	"eth_estimateGasAfterCalls": ClassEVMExecution,

	// Transaction submission.
	"eth_sendRawTransaction": ClassBroadcast,
	"eth_sendTransaction":    ClassBroadcast,

	// Long-lived EVM requests.
	"eth_subscribe":   ClassSubscription,
	"eth_unsubscribe": ClassSubscription,

	// CometBFT metadata and small status reads.
	"abci_info":           ClassCheapRead,
	"health":              ClassCheapRead,
	"lag_status":          ClassCheapRead,
	"status":              ClassCheapRead,
	"num_unconfirmed_txs": ClassCheapRead,

	// CometBFT index scans.
	"block_search": ClassSearchIndex,
	"tx":           ClassSearchIndex,
	"tx_search":    ClassSearchIndex,

	// CometBFT methods that construct collections of blocks or transactions.
	"block":           ClassBlockTxMaterialization,
	"block_by_hash":   ClassBlockTxMaterialization,
	"block_results":   ClassBlockTxMaterialization,
	"blockchain":      ClassBlockTxMaterialization,
	"genesis":         ClassBlockTxMaterialization,
	"genesis_chunked": ClassBlockTxMaterialization,
	"unconfirmed_txs": ClassBlockTxMaterialization,

	// CometBFT submission methods.
	"broadcast_evidence":   ClassBroadcast,
	"broadcast_tx":         ClassBroadcast,
	"broadcast_tx_async":   ClassBroadcast,
	"broadcast_tx_commit":  ClassBroadcast,
	"broadcast_tx_sync":    ClassBroadcast,
	"check_tx":             ClassBroadcast,
	"unsafe_flush_mempool": ClassBroadcast,

	// CometBFT long-lived requests.
	"events":          ClassSubscription,
	"subscribe":       ClassSubscription,
	"unsubscribe":     ClassSubscription,
	"unsubscribe_all": ClassSubscription,
}

// ClassifyMethod returns the resource class for a JSON-RPC or full gRPC method name.
// Unlisted methods are normal reads.
func ClassifyMethod(method string) MethodClass {
	method = strings.TrimPrefix(method, "/")
	if class, ok := exactMethodClasses[method]; ok {
		return class
	}
	if strings.HasPrefix(method, "debug_trace") || strings.HasPrefix(method, "trace_") ||
		strings.HasPrefix(method, "sei_trace") {
		return ClassTrace
	}

	// gRPC exposes /package.Service/Method. Match only the final component, so
	// package and service renames do not change the resource class.
	if slash := strings.LastIndexByte(method, '/'); slash >= 0 {
		return classifyGRPCMethod(method[slash+1:])
	}
	return ClassNormalRead
}

func classifyGRPCMethod(method string) MethodClass {
	switch method {
	case "BroadcastTx", "BroadcastTxAsync", "BroadcastTxCommit", "BroadcastTxSync":
		return ClassBroadcast
	case "Subscribe", "Unsubscribe":
		return ClassSubscription
	case "Simulate":
		return ClassEVMExecution
	case "GetBlockByHeight", "GetBlockWithTxs", "GetLatestBlock", "GetLatestValidatorSet", "GetValidatorSetByHeight":
		return ClassBlockTxMaterialization
	case "GetTx", "GetTxsEvent":
		return ClassSearchIndex
	}
	return ClassNormalRead
}

// Weight returns the global admission units consumed by one method in the class.
func (c MethodClass) Weight() int64 {
	switch c {
	case ClassCheapRead, ClassSubscription:
		return 1
	case ClassNormalRead, ClassBroadcast:
		return 2
	case ClassSearchIndex, ClassBlockTxMaterialization:
		return 8
	case ClassLogQuery, ClassEVMExecution:
		return 10
	case ClassTrace:
		return 20
	default:
		return ClassNormalRead.Weight()
	}
}

func (c MethodClass) valid() bool {
	for _, known := range allMethodClasses {
		if c == known {
			return true
		}
	}
	return false
}

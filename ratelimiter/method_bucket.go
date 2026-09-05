package ratelimiter

import "strings"

const (
	// PlaneCometBFT is the rate-limit plane label for Tendermint RPC HTTP.
	PlaneCometBFT = "cometbft"
	// PlaneGRPC is the rate-limit plane label for native gRPC (:9090).
	PlaneGRPC = "grpc"
	// PlaneGRPCWeb is the plane label for gRPC-Web (:9091) connection
	// rejections. The two planes share PlaneGRPC everywhere they share a
	// per-IP pool; the connection cap is configured and enforced per listener,
	// so its metric names the listener that refused.
	PlaneGRPCWeb = "grpc-web"

	// rpcMethodBucketOther is the fallback label for unrecognized methods.
	rpcMethodBucketOther = "other"
	// MethodInvalid labels rate-limit charges for unparseable request bodies.
	MethodInvalid = "invalid"
	// maxRPCMethodLen rejects oversized method strings before metric recording.
	maxRPCMethodLen = 128
)

// knownRPCNamespaces lists JSON-RPC namespaces the node may expose. Rejection
// metrics record the namespace (the prefix before the first '_') rather than the
// full method string, keeping OTel attribute cardinality bounded.
var knownRPCNamespaces = map[string]struct{}{
	"abci":     {},
	"admin":    {},
	"debug":    {},
	"engine":   {},
	"eth":      {},
	"miner":    {},
	"net":      {},
	"personal": {},
	"sei":      {},
	"trace":    {},
	"txpool":   {},
	"web3":     {},
}

// knownCometBFTRPCMethods lists Tendermint RPC method names registered by the
// node. Rejection metrics on PlaneCometBFT record the method name directly
// rather than an EVM-style namespace prefix.
var knownCometBFTRPCMethods = map[string]struct{}{
	"abci_info":            {},
	"abci_query":           {},
	"block":                {},
	"block_by_hash":        {},
	"block_results":        {},
	"block_search":         {},
	"blockchain":           {},
	"broadcast_evidence":   {},
	"broadcast_tx":         {},
	"broadcast_tx_async":   {},
	"broadcast_tx_commit":  {},
	"broadcast_tx_sync":    {},
	"catalog":              {},
	"check_tx":             {},
	"commit":               {},
	"consensus_params":     {},
	"consensus_state":      {},
	"dump_consensus_state": {},
	"events":               {},
	"genesis":              {},
	"genesis_chunked":      {},
	"header":               {},
	"header_by_hash":       {},
	"health":               {},
	"lag_status":           {},
	"net_info":             {},
	"num_unconfirmed_txs":  {},
	"status":               {},
	"subscribe":            {},
	"tx":                   {},
	"tx_search":            {},
	"unconfirmed_txs":      {},
	"unsubscribe":          {},
	"unsubscribe_all":      {},
	"unsafe_flush_mempool": {},
	"validators":           {},
	"websocket":            {},
}

// bucketRPCMethod maps a raw JSON-RPC method name to a low-cardinality label
// suitable for OTel/Prometheus metrics. Attacker-controlled method strings
// collapse to rpcMethodBucketOther. knownGRPCMethods bounds the PlaneGRPC label
// set; the other planes ignore it.
func bucketRPCMethod(plane, method string, knownGRPCMethods map[string]struct{}) string {
	if method == MethodInvalid {
		return MethodInvalid
	}
	if plane == PlaneCometBFT {
		return bucketCometBFTRPCMethod(method)
	}
	if plane == PlaneGRPC {
		return bucketGRPCMethod(method, knownGRPCMethods)
	}
	return bucketNamespacedRPCMethod(method)
}

func bucketGRPCMethod(fullMethod string, known map[string]struct{}) string {
	if fullMethod == "" || len(fullMethod) > maxRPCMethodLen {
		return rpcMethodBucketOther
	}
	name := strings.TrimPrefix(fullMethod, "/")
	if _, ok := known[name]; ok {
		return name
	}
	return rpcMethodBucketOther
}

func bucketCometBFTRPCMethod(method string) string {
	if method == "" || len(method) > maxRPCMethodLen {
		return rpcMethodBucketOther
	}
	if _, ok := knownCometBFTRPCMethods[method]; ok {
		return method
	}
	return rpcMethodBucketOther
}

func bucketNamespacedRPCMethod(method string) string {
	if method == "" || len(method) > maxRPCMethodLen {
		return rpcMethodBucketOther
	}
	underscore := strings.IndexByte(method, '_')
	if underscore <= 0 || underscore >= len(method)-1 {
		return rpcMethodBucketOther
	}
	ns := method[:underscore]
	if _, ok := knownRPCNamespaces[ns]; !ok {
		return rpcMethodBucketOther
	}
	suffix := method[underscore+1:]
	for i := 0; i < len(suffix); i++ {
		c := suffix[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return rpcMethodBucketOther
		}
	}
	return ns
}

package ratelimiter

import "strings"

const (
	// rpcMethodBucketOther is the fallback label for unrecognized or malformed methods.
	rpcMethodBucketOther = "other"
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
	"sei2":     {},
	"trace":    {},
	"txpool":   {},
	"web3":     {},
}

// bucketRPCMethod maps a raw JSON-RPC method name to a low-cardinality label
// suitable for OTel/Prometheus metrics. Attacker-controlled method strings
// collapse to rpcMethodBucketOther.
func bucketRPCMethod(method string) string {
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

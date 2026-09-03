package ratelimiter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBucketRPCMethod_KnownNamespaces(t *testing.T) {
	require.Equal(t, "eth", bucketRPCMethod("evm", "eth_call", nil))
	require.Equal(t, "eth", bucketRPCMethod("evm", "eth_getBalance", nil))
	require.Equal(t, "debug", bucketRPCMethod("evm", "debug_traceTransaction", nil))
	require.Equal(t, "web3", bucketRPCMethod("evm", "web3_clientVersion", nil))
}

func TestBucketRPCMethod_UnknownOrMalformed(t *testing.T) {
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "notnamespaced", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "eth", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "_eth_call", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "eth_", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "bogus_method", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "eth_BAD-chars", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", strings.Repeat("a", maxRPCMethodLen+1), nil))
}

func TestBucketRPCMethod_ValidAndInvalidMethods(t *testing.T) {
	seen := make(map[string]struct{}, 3)
	for _, method := range []string{
		"eth_call",
		"eth_random-uuid-1",
		"eth_random-uuid-2",
	} {
		seen[bucketRPCMethod("evm", method, nil)] = struct{}{}
	}
	require.Len(t, seen, 2)
	require.Contains(t, seen, "eth")
	require.Contains(t, seen, rpcMethodBucketOther)
}

func TestBucketRPCMethod_CometBFTKnownMethods(t *testing.T) {
	for _, method := range []string{
		"status",
		"tx_search",
		"block_search",
		"broadcast_tx",
		"catalog",
		"websocket",
	} {
		require.Equal(t, method, bucketRPCMethod(PlaneCometBFT, method, nil), method)
	}
}

func TestBucketRPCMethod_CometBFTUnknownMethods(t *testing.T) {
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneCometBFT, "", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneCometBFT, "bogus", nil))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneCometBFT, strings.Repeat("a", maxRPCMethodLen+1), nil))
}

func TestBucketRPCMethod_Invalid(t *testing.T) {
	require.Equal(t, MethodInvalid, bucketRPCMethod("evm", MethodInvalid, nil))
	require.Equal(t, MethodInvalid, bucketRPCMethod(PlaneCometBFT, MethodInvalid, nil))
	require.Equal(t, MethodInvalid, bucketRPCMethod(PlaneGRPC, MethodInvalid, nil))
}

func TestBucketRPCMethod_GrpcKnownMethods(t *testing.T) {
	known := map[string]struct{}{
		"cosmos.bank.v1beta1.Query/Balance":  {},
		"cosmos.tx.v1beta1.Service/Simulate": {},
	}
	require.Equal(t, "cosmos.bank.v1beta1.Query/Balance", bucketRPCMethod(PlaneGRPC, "/cosmos.bank.v1beta1.Query/Balance", known))
	require.Equal(t, "cosmos.tx.v1beta1.Service/Simulate", bucketRPCMethod(PlaneGRPC, "/cosmos.tx.v1beta1.Service/Simulate", known))
}

func TestBucketRPCMethod_GrpcUnknownMethods(t *testing.T) {
	known := map[string]struct{}{"cosmos.bank.v1beta1.Query/Balance": {}}
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, "", known))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, "/cosmos.bank.v1beta1.Query/AllBalances", known))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, "/bogus.Service/Call", known))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, "not-a-grpc-path", known))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, strings.Repeat("a", maxRPCMethodLen+1), known))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, "/cosmos.bank.v1beta1.Query/Balance", nil))
}

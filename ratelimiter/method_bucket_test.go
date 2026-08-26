package ratelimiter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBucketRPCMethod_KnownNamespaces(t *testing.T) {
	require.Equal(t, "eth", bucketRPCMethod("evm", "eth_call"))
	require.Equal(t, "eth", bucketRPCMethod("evm", "eth_getBalance"))
	require.Equal(t, "debug", bucketRPCMethod("evm", "debug_traceTransaction"))
	require.Equal(t, "web3", bucketRPCMethod("evm", "web3_clientVersion"))
}

func TestBucketRPCMethod_UnknownOrMalformed(t *testing.T) {
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", ""))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "notnamespaced"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "eth"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "_eth_call"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "eth_"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "bogus_method"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", "eth_BAD-chars"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("evm", strings.Repeat("a", maxRPCMethodLen+1)))
}

func TestBucketRPCMethod_ValidAndInvalidMethods(t *testing.T) {
	seen := make(map[string]struct{}, 3)
	for _, method := range []string{
		"eth_call",
		"eth_random-uuid-1",
		"eth_random-uuid-2",
	} {
		seen[bucketRPCMethod("evm", method)] = struct{}{}
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
		require.Equal(t, method, bucketRPCMethod(PlaneCometBFT, method), method)
	}
}

func TestBucketRPCMethod_CometBFTUnknownMethods(t *testing.T) {
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneCometBFT, ""))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneCometBFT, "bogus"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneCometBFT, strings.Repeat("a", maxRPCMethodLen+1)))
}

func TestBucketRPCMethod_Invalid(t *testing.T) {
	require.Equal(t, MethodInvalid, bucketRPCMethod("evm", MethodInvalid))
	require.Equal(t, MethodInvalid, bucketRPCMethod(PlaneCometBFT, MethodInvalid))
	require.Equal(t, MethodInvalid, bucketRPCMethod(PlaneGRPC, MethodInvalid))
}

func TestBucketRPCMethod_GrpcknownServices(t *testing.T) {
	require.Equal(t, "cosmos.bank.v1beta1.Query", bucketRPCMethod(PlaneGRPC, "/cosmos.bank.v1beta1.Query/Balance"))
	require.Equal(t, "cosmos.tx.v1beta1.Service", bucketRPCMethod(PlaneGRPC, "/cosmos.tx.v1beta1.Service/Simulate"))
}

func TestBucketRPCMethod_GrpcUnknownServices(t *testing.T) {
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, ""))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, "/bogus.Service/Call"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, "not-a-grpc-path"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(PlaneGRPC, strings.Repeat("a", maxRPCMethodLen+1)))
}

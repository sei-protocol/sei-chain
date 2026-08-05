package ratelimiter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBucketRPCMethod_KnownNamespaces(t *testing.T) {
	require.Equal(t, "eth", bucketRPCMethod("eth_call"))
	require.Equal(t, "eth", bucketRPCMethod("eth_getBalance"))
	require.Equal(t, "debug", bucketRPCMethod("debug_traceTransaction"))
	require.Equal(t, "web3", bucketRPCMethod("web3_clientVersion"))
	require.Equal(t, "sei2", bucketRPCMethod("sei2_getBlock"))
}

func TestBucketRPCMethod_UnknownOrMalformed(t *testing.T) {
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(""))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("notnamespaced"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("eth"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("_eth_call"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("eth_"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("bogus_method"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod("eth_BAD-chars"))
	require.Equal(t, rpcMethodBucketOther, bucketRPCMethod(strings.Repeat("a", maxRPCMethodLen+1)))
}

func TestBucketRPCMethod_ValidAndInvalidMethods(t *testing.T) {
	seen := make(map[string]struct{}, 3)
	for _, method := range []string{
		"eth_call",
		"eth_random-uuid-1",
		"eth_random-uuid-2",
	} {
		seen[bucketRPCMethod(method)] = struct{}{}
	}
	require.Len(t, seen, 2)
	require.Contains(t, seen, "eth")
	require.Contains(t, seen, rpcMethodBucketOther)
}

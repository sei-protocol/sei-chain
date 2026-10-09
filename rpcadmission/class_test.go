package rpcadmission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyMethod(t *testing.T) {
	tests := []struct {
		method string
		want   MethodClass
	}{
		{"eth_chainId", ClassCheapRead},
		{"eth_getBalance", ClassNormalRead},
		{"eth_getTransactionByHash", ClassSearchIndex},
		{"tx_search", ClassSearchIndex},
		{"eth_getBlockByNumber", ClassBlockTxMaterialization},
		{"eth_getBlockTransactionCountByHash", ClassBlockTxMaterialization},
		{"block_results", ClassBlockTxMaterialization},
		{"eth_getLogs", ClassLogQuery},
		{"eth_call", ClassEVMExecution},
		{"debug_traceTransaction", ClassTrace},
		{"trace_block", ClassTrace},
		{"eth_sendRawTransaction", ClassBroadcast},
		{"broadcast_tx_sync", ClassBroadcast},
		{"unsafe_flush_mempool", ClassBroadcast},
		{"eth_subscribe", ClassSubscription},
		{"subscribe", ClassSubscription},
		{"/cosmos.tx.v1beta1.Service/Simulate", ClassEVMExecution},
		{"/cosmos.tx.v1beta1.Service/BroadcastTx", ClassBroadcast},
		{"/cosmos.tx.v1beta1.Service/GetTxsEvent", ClassSearchIndex},
		{"/cosmos.base.tendermint.v1beta1.Service/GetBlockByHeight", ClassBlockTxMaterialization},
		{"/cosmos.bank.v1beta1.Query/Balance", ClassNormalRead},
		{"attacker_controlled", ClassNormalRead},
		{"", ClassNormalRead},
	}
	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			require.Equal(t, test.want, ClassifyMethod(test.method))
		})
	}
}

func TestMethodClassWeights(t *testing.T) {
	want := map[MethodClass]int64{
		ClassCheapRead:              1,
		ClassNormalRead:             2,
		ClassSearchIndex:            8,
		ClassBlockTxMaterialization: 8,
		ClassLogQuery:               10,
		ClassEVMExecution:           10,
		ClassTrace:                  20,
		ClassBroadcast:              2,
		ClassSubscription:           1,
	}
	for _, class := range allMethodClasses {
		require.Equal(t, want[class], class.Weight(), class)
	}
	require.Equal(t, ClassNormalRead.Weight(), MethodClass("unknown").Weight())
}

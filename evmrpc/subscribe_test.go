package evmrpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryBuilder(t *testing.T) {
	q := mockQueryBuilder()
	expectedQuery := "tm.event = 'Tx' AND evm_log.contract_address = 'test contract' AND evm_log.block_hash = 'block hash' AND evm_log.block_number = '1' AND evm_log.tx_hash = 'tx hash' AND evm_log.tx_idx = '2' AND evm_log.idx = '3' AND evm_log.topics CONTAINS 'topic a' AND evm_log.topics CONTAINS 'topic b'"
	require.Equal(t, expectedQuery, q.Build())
}

func TestSubscribe(t *testing.T) {
	manager := NewSubscriptionManager(&MockClient{})
	res, err := manager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.Nil(t, err)
	require.Equal(t, 1, int(res))

	res, err = manager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.Nil(t, err)
	require.Equal(t, 2, int(res))

	badManager := NewSubscriptionManager(&MockBadClient{})
	_, err = badManager.Subscribe(context.Background(), mockQueryBuilder(), 10)
	require.NotNil(t, err)
}

func mockQueryBuilder() *QueryBuilder {
	q := NewQueryBuilder()
	q.FilterContractAddress("test contract")
	q.FilterBlockHash("block hash")
	q.FilterBlockNumber(1)
	q.FilterTxHash("tx hash")
	q.FilterTxIndex(2)
	q.FilterIndex(3)
	q.FilterTopic("topic a")
	q.FilterTopic("topic b")
	return q
}

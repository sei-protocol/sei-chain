package evmrpc_test

import (
	"regexp"
	"testing"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestQueryBuilder(t *testing.T) {
	q := mockQueryBuilder()
	expectedQuery := "tm.event = 'Tx' AND evm_log.contract_address = 'test contract' AND evm_log.block_hash = 'block hash' AND evm_log.block_number = '1' AND evm_log.tx_hash = 'tx hash' AND evm_log.tx_idx = '2' AND evm_log.idx = '3' AND evm_log.topics CONTAINS 'topic a' AND evm_log.topics CONTAINS 'topic b'"
	require.Equal(t, expectedQuery, q.Build())
}

func mockQueryBuilder() *evmrpc.QueryBuilder {
	q := evmrpc.NewTxQueryBuilder()
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

func TestGetTopicsRegex(t *testing.T) {
	tests := []struct {
		name         string
		topics       [][]string
		wantErr      bool
		wantMatch    []string
		wantNotMatch []string
	}{
		{
			name:    "error: topics length 0",
			topics:  [][]string{},
			wantErr: true,
		},
		{
			name:         "match first topic",
			topics:       [][]string{{"a"}},
			wantErr:      false,
			wantMatch:    []string{"[a]", "[a,b]", "[a,a,a,a]"},
			wantNotMatch: []string{"b", "[b]", "[b,a]", "[a,b"},
		},
		{
			name:         "match first topic with OR",
			topics:       [][]string{{"a", "b"}}, // first topic can be a or b
			wantErr:      false,
			wantMatch:    []string{"[a]", "[a,b]", "[a,c,c,c]", "[b]", "[b,c]", "[b,c,c,c]"},
			wantNotMatch: []string{"b", "[c]", "[c,a]", "[c,b"},
		},
		{
			name:         "match second topic",
			topics:       [][]string{{}, {"a"}},
			wantErr:      false,
			wantMatch:    []string{"[b,a]", "[c,a]", "[a,a,a]"},
			wantNotMatch: []string{"b,a]", "[a,b,a]"},
		},
		{
			name:         "match second and fourth topic",
			topics:       [][]string{{""}, {"a", "c"}, {""}, {"b", "d"}},
			wantErr:      false,
			wantMatch:    []string{"[d,a,c,b]", "[c,a,c,d,c]", "[a,c,b,d]"},
			wantNotMatch: []string{"[a,a,a,a]", "[a,b]", "[c,a,b,c]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evmrpc.GetTopicsRegex(tt.topics)
			regex := regexp.MustCompile(got)
			if tt.wantErr {
				require.NotNil(t, err)
				return
			}
			require.Nil(t, err)
			for _, toMatch := range tt.wantMatch {
				require.True(t, regex.MatchString(toMatch))
			}
			for _, toNotMatch := range tt.wantNotMatch {
				require.False(t, regex.MatchString(toNotMatch))
			}
		})
	}
}

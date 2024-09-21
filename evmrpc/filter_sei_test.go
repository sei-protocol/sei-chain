package evmrpc_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestSeiFilterGetLogs(t *testing.T) {
	// make sure we pass all the eth_ namespace tests
	// TODO: uncomment
	testFilterGetLogs(t, "sei", getCommonFilterLogTests())

	// test where we get a synthetic log
	testFilterGetLogs(t, "sei", []GetFilterLogTests{
		{
			name:      "filter by single synthetic address",
			fromBlock: "0x8",
			toBlock:   "0x8",
			addrs:     []common.Address{common.HexToAddress("0x1111111111111111111111111111111111111116")},
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x1111111111111111111111111111111111111116", log["address"].(string))
			},
			wantLen: 1,
		},
		{
			name:    "filter by single topic with default range, include synethetic logs",
			topics:  [][]common.Hash{{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000234")}},
			wantErr: false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000234", log["topics"].([]interface{})[0].(string))
			},
			wantLen: 1,
		},
	})
}
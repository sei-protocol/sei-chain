package evmrpc

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestGetTxReceipt(t *testing.T) {
	types.RegisterInterfaces(EncodingConfig.InterfaceRegistry)

	require.Nil(t, EVMKeeper.SetReceipt(Ctx, common.HexToHash("0x1234567890123456789012345678901234567890123456789012345678901234"), &types.Receipt{
		From:              "0x123456789012345678902345678901234567890",
		To:                "0x123456789012345678902345678901234567890",
		TransactionIndex:  0,
		BlockNumber:       8,
		TxType:            1,
		ContractAddress:   "0x123456789012345678902345678901234567890",
		CumulativeGasUsed: 123,
		TxHashHex:         "0x123456789012345678902345678901234567890123456789012345678901234",
		GasUsed:           55,
		Status:            0,
		EffectiveGasPrice: 10,
	}))

	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionReceipt\",\"params\":[\"0x1234567890123456789012345678901234567890123456789012345678901234\"],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BlockTestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":{\"blockHash\":\"0x3030303030303030303030303030303030303030303030303030303030303031\",\"blockNumber\":\"0x8\",\"contractAddress\":\"0x0123456789012345678902345678901234567890\",\"cumulativeGasUsed\":\"0x7b\",\"effectiveGasPrice\":\"0xa\",\"from\":\"0x0123456789012345678902345678901234567890\",\"gasUsed\":\"0x37\",\"logs\":[{\"address\":\"0x1111111111111111111111111111111111111111\",\"topics\":[\"0x1111111111111111111111111111111111111111111111111111111111111111\",\"0x1111111111111111111111111111111111111111111111111111111111111112\"],\"data\":\"0x78797a\",\"blockNumber\":\"0x8\",\"transactionHash\":\"0x1111111111111111111111111111111111111111111111111111111111111113\",\"transactionIndex\":\"0x2\",\"blockHash\":\"0x1111111111111111111111111111111111111111111111111111111111111111\",\"logIndex\":\"0x1\",\"removed\":true}],\"logsBloom\":\"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000\",\"status\":\"0x0\",\"to\":\"0x0123456789012345678902345678901234567890\",\"transactionHash\":\"0x0123456789012345678902345678901234567890123456789012345678901234\",\"transactionIndex\":\"0x0\",\"type\":\"0x1\"}}\n", string(resBody))

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, BadBlockTestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"error\":{\"code\":-32000,\"message\":\"error block\"}}\n", string(resBody))
}

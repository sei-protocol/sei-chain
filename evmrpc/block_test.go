package evmrpc

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetBlockByHash(t *testing.T) {
	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockByHash\",\"params\":[\"0x0000000000000000000000000000000000000000000000000000000000000001\",true],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":{\"difficulty\":\"0x0\",\"extraData\":\"0x\",\"gasLimit\":\"0xa\",\"gasUsed\":\"0x5\",\"hash\":\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"logsBloom\":\"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000\",\"miner\":\"0x0000000000000000000000000000000000000005\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"nonce\":\"0x0000000000000000\",\"number\":\"0x8\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000006\",\"receiptsRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000004\",\"sha3Uncles\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"size\":\"0x260\",\"stateRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000003\",\"timestamp\":\"0x65254651\",\"transactions\":[{\"blockHash\":\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"blockNumber\":\"0x8\",\"from\":\"0x1234567890123456789012345678901234567890\",\"gas\":\"0x3e8\",\"gasPrice\":\"0xa\",\"maxFeePerGas\":\"0xa\",\"maxPriorityFeePerGas\":\"0x0\",\"hash\":\"0x78b0bd7fe9ccc8ae8a61eae9315586cf2a406dacf129313e6c5769db7cd14372\",\"input\":\"0x616263\",\"nonce\":\"0x1\",\"to\":\"0x0000000000000000000000000000000000010203\",\"transactionIndex\":\"0x0\",\"value\":\"0x3e8\",\"type\":\"0x0\",\"accessList\":[],\"chainId\":\"0x1\",\"v\":\"0x1\",\"r\":\"0x34125c09c6b1a57f5f571a242572129057b22612dd56ee3519c4f68bece0db03\",\"s\":\"0x3f4fe6f2512219bac6f9b4e4be1aa11d3ef79c5c2f1000ef6fa37389de0ff523\",\"yParity\":\"0x1\"}],\"transactionsRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000002\",\"uncles\":[]}}\n", string(resBody))
}

func TestGetBlockByNumber(t *testing.T) {
	for _, num := range []string{"0x8", "earliest", "latest", "pending", "finalized", "safe"} {
		body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockByNumber\",\"params\":[\"%s\",true],\"id\":\"test\"}", num)
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":{\"difficulty\":\"0x0\",\"extraData\":\"0x\",\"gasLimit\":\"0xa\",\"gasUsed\":\"0x5\",\"hash\":\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"logsBloom\":\"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000\",\"miner\":\"0x0000000000000000000000000000000000000005\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"nonce\":\"0x0000000000000000\",\"number\":\"0x8\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000006\",\"receiptsRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000004\",\"sha3Uncles\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"size\":\"0x260\",\"stateRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000003\",\"timestamp\":\"0x65254651\",\"transactions\":[{\"blockHash\":\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"blockNumber\":\"0x8\",\"from\":\"0x1234567890123456789012345678901234567890\",\"gas\":\"0x3e8\",\"gasPrice\":\"0xa\",\"maxFeePerGas\":\"0xa\",\"maxPriorityFeePerGas\":\"0x0\",\"hash\":\"0x78b0bd7fe9ccc8ae8a61eae9315586cf2a406dacf129313e6c5769db7cd14372\",\"input\":\"0x616263\",\"nonce\":\"0x1\",\"to\":\"0x0000000000000000000000000000000000010203\",\"transactionIndex\":\"0x0\",\"value\":\"0x3e8\",\"type\":\"0x0\",\"accessList\":[],\"chainId\":\"0x1\",\"v\":\"0x1\",\"r\":\"0x34125c09c6b1a57f5f571a242572129057b22612dd56ee3519c4f68bece0db03\",\"s\":\"0x3f4fe6f2512219bac6f9b4e4be1aa11d3ef79c5c2f1000ef6fa37389de0ff523\",\"yParity\":\"0x1\"}],\"transactionsRoot\":\"0x0000000000000000000000000000000000000000000000000000000000000002\",\"uncles\":[]}}\n", string(resBody))
	}

	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockByNumber\",\"params\":[\"bad_num\",true],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"error\":{\"code\":-32602,\"message\":\"invalid argument 0: hex string without 0x prefix\"}}\n", string(resBody))
}

func TestGetBlockTransactionCount(t *testing.T) {
	// get by block number
	for _, num := range []string{"0x8", "earliest", "latest", "pending", "finalized", "safe"} {
		body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByNumber\",\"params\":[\"%s\"],\"id\":\"test\"}", num)
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":\"0x1\"}\n", string(resBody))
	}

	// get error returns null
	body := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByNumber\",\"params\":[\"0x8\"],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":null}\n", string(resBody))
	body = "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByNumber\",\"params\":[\"earliest\"],\"id\":\"test\"}"
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":null}\n", string(resBody))
	body = "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByHash\",\"params\":[\"0x0000000000000000000000000000000000000000000000000000000000000001\"],\"id\":\"test\"}"
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":null}\n", string(resBody))

	// get by hash
	body = "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBlockTransactionCountByHash\",\"params\":[\"0x0000000000000000000000000000000000000000000000000000000000000001\"],\"id\":\"test\"}"
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":\"0x1\"}\n", string(resBody))
}

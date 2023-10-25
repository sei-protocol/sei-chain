package evmrpc

import (
	// "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestNewFilter(t *testing.T) {
	tests := []struct {
		name      string
		fromBlock string
		toBlock   string
		addrs     []common.Address
		topics    []common.Hash
		wantErr   bool
		wantId    float64
	}{
		{
			name:      "happy path",
			fromBlock: "0x1",
			toBlock:   "0x2",
			addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
			topics:    []common.Hash{common.HexToHash(common.Bytes2Hex([]byte("topic")))},
			wantErr:   false,
			wantId:    1,
		},
		{
			name:      "from block after to block",
			fromBlock: "0x2",
			toBlock:   "0x1",
			addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
			topics:    []common.Hash{common.HexToHash(common.Bytes2Hex([]byte("topic")))},
			wantErr:   true,
			wantId:    0,
		},
		{
			name:      "from block is latest but to block is not",
			fromBlock: "latest",
			toBlock:   "0x1",
			addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
			topics:    []common.Hash{common.HexToHash(common.Bytes2Hex([]byte("topic")))},
			wantErr:   true,
			wantId:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addrsStrs := []string{}
			for _, addr := range tt.addrs {
				addrsStrs = append(addrsStrs, "\""+addr.String()+"\"")
			}
			topicsStrs := []string{}
			for _, topic := range tt.topics {
				topicsStrs = append(topicsStrs, "\""+topic.String()+"\"")
			}
			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_newFilter\",\"params\":[\"%s\",\"%s\",%s,%s],\"id\":\"test\"}", tt.fromBlock, tt.toBlock, addrsStrs, topicsStrs)
			fmt.Println("body = ", body)
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
			require.Nil(t, err)
			req.Header.Set("Content-Type", "application/json")
			f := func() map[string]interface{} {
				res, err := http.DefaultClient.Do(req)
				require.Nil(t, err)
				resBody, err := io.ReadAll(res.Body)
				require.Nil(t, err)
				resObj := map[string]interface{}{}
				require.Nil(t, json.Unmarshal(resBody, &resObj))
				return resObj
			}
			resObj := f()
			if tt.wantErr {
				_, ok := resObj["error"]
				require.True(t, ok)
			} else {
				got := resObj["result"].(float64)
				require.Equal(t, tt.wantId, got)
				// check that filter id increments
				resObj := f()
				got2 := resObj["result"].(float64)
				require.Equal(t, tt.wantId+1, got2)
			}
		})
	}
}

func TestUninstallFilter(t *testing.T) {
	// uninstall existing filter
	emptyArr := []string{}
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_newFilter\",\"params\":[\"%s\",\"%s\",%s,%s],\"id\":\"test\"}", "0x1", "0xa", emptyArr, emptyArr)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	filterId := int(resObj["result"].(float64))
	require.Equal(t, 1, filterId)
	body = fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_uninstallFilter\",\"params\":[%d],\"id\":\"test\"}", filterId)
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	uninstallSuccess := resObj["result"].(bool)
	require.True(t, uninstallSuccess)

	// uninstall non-existing filter
	nonExistingFilterId := 100
	body = fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_uninstallFilter\",\"params\":[%d],\"id\":\"test\"}", nonExistingFilterId)
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	uninstallSuccess = resObj["result"].(bool)
	require.False(t, uninstallSuccess)
}

func TestGetLogs(t *testing.T) {
	emptyArr := []string{}
	// [done] test error: block hash and from + to block both given
	// [done] test error: from block is after to block
	// [skip] test error: block hash and from + to block -- none given
	// [done] test success: only block hash given -- function correctly filters out block hash
	// test success: only from and to block given -- function correctly filters out events not in range
	// test success: function correctly filters out by address
	// test success: filter by topic
	tests := []struct {
		name      string
		blockHash string
		fromBlock string
		toBlock   string
		addrs     []common.Address
		topics    []common.Hash
		wantErr   bool
		wantLen   int
		check     func(t *testing.T, log map[string]interface{})
	}{
		{
			name:      "error: block hash and block range both given",
			blockHash: "0x1111111111111111111111111111111111111111111111111111111111111111",
			fromBlock: "0x1",
			toBlock:   "0x2",
			addrs:     []common.Address{},
			topics:    []common.Hash{},
			wantErr:   true,
		},
		{
			name:      "error: from block after to block",
			blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
			fromBlock: "0x2",
			toBlock:   "0x1",
			addrs:     []common.Address{},
			topics:    []common.Hash{},
			wantErr:   true,
		},
		// having a bit of trouble specifying block range not given
		// {
		// 	name:      "error: neither block hash nor block range given",
		// 	blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
		// 	fromBlock: "0x0",
		// 	toBlock:   "0x0",
		// 	addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
		// 	topics:    []common.Hash{common.HexToHash(common.Bytes2Hex([]byte("topic")))},
		// 	wantErr:   true,
		// 	wantLen:   0,
		// },
		{
			name:      "success: only block hash given",
			blockHash: "0x1111111111111111111111111111111111111111111111111111111111111111",
			fromBlock: "0x0",
			toBlock:   "0x0",
			addrs:     []common.Address{},
			topics:    []common.Hash{},
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, log["blockHash"], "0x1111111111111111111111111111111111111111111111111111111111111111")
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addrsStrs := []string{}
			for _, addr := range tt.addrs {
				addrsStrs = append(addrsStrs, "\""+addr.String()+"\"")
			}
			// blockHash := "0x78b0bd7fe9ccc8ae8a61eae9315586cf2a406dacf129313e6c5769db7cd14372"
			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getLogs\",\"params\":[\"%s\",%s,\"%s\",\"%s\",%s],\"id\":\"test\"}", tt.blockHash, addrsStrs, tt.fromBlock, tt.toBlock, emptyArr)
			fmt.Println("body = ", body)
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
			require.Nil(t, err)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			require.Nil(t, err)
			resBody, err := io.ReadAll(res.Body)
			require.Nil(t, err)
			resObj := map[string]interface{}{}
			require.Nil(t, json.Unmarshal(resBody, &resObj))
			fmt.Println("resObj = ", resObj)
			if tt.wantErr {
				_, ok := resObj["error"]
				require.True(t, ok)
			} else {
				got := resObj["result"].([]interface{})
				for _, log := range got {
					logObj := log.(map[string]interface{})
					fmt.Println("logObj = ", logObj)
					tt.check(t, logObj)
				}
				require.GreaterOrEqual(t, len(got), tt.wantLen)
			}
		})
	}

}

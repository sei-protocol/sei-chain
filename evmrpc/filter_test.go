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

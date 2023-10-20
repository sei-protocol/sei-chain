package evmrpc

import (
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
			topics:    []common.Hash{},
			wantErr:   false,
			wantId:    1,
		},
		{
			name:      "from block after to block",
			fromBlock: "2",
			toBlock:   "1",
			addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
			topics:    []common.Hash{},
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
			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_newFilter\",\"params\":[\"%s\",\"%s\",%v,%s],\"id\":\"test\"}", tt.fromBlock, tt.toBlock, addrsStrs, tt.topics)
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
			require.Nil(t, err)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			require.Nil(t, err)
			resBody, err := io.ReadAll(res.Body)
			require.Nil(t, err)
			resObj := map[string]interface{}{}
			require.Nil(t, json.Unmarshal(resBody, &resObj))
			if tt.wantErr {
				_, ok := resObj["error"]
				require.True(t, ok)
			} else {
				fmt.Println("resObj = ", resObj)
				got := resObj["result"].(float64)
				require.Equal(t, tt.wantId, got)
			}
		})
	}
}

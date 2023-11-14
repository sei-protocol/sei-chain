package evmrpc_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGetBalance(t *testing.T) {
	tests := []struct {
		name       string
		addr       string
		blockNr    string
		wantErr    bool
		wantAmount string
	}{
		{
			name:       "latest block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "latest",
			wantErr:    false,
			wantAmount: "0x38d7ea4c68000",
		},
		{
			name:       "safe block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "safe",
			wantErr:    false,
			wantAmount: "0x38d7ea4c68000",
		},
		{
			name:       "finalized block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "finalized",
			wantErr:    false,
			wantAmount: "0x38d7ea4c68000",
		},
		{
			name:       "pending block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "pending",
			wantErr:    false,
			wantAmount: "0x38d7ea4c68000",
		},
		{
			name:       "evm address with sei address mapping",
			addr:       common.HexToAddress(common.Bytes2Hex([]byte("evmAddr"))).String(),
			blockNr:    "latest",
			wantErr:    false,
			wantAmount: "0x9184e72a000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getBalance\",\"params\":[\"%s\",\"%s\"],\"id\":\"test\"}", tt.addr, tt.blockNr)
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
				_, ok := resObj["error"]
				require.False(t, ok)
				require.Equal(t, tt.wantAmount, resObj["result"])
			}
		})
	}
}

func TestGetCode(t *testing.T) {
	wantKey := "0x" + hex.EncodeToString([]byte("abc"))
	tests := []struct {
		name    string
		blockNr string
		wantErr bool
	}{
		{
			name:    "latest block",
			blockNr: "latest",
			wantErr: false,
		},
		{
			name:    "safe block",
			blockNr: "safe",
			wantErr: false,
		},
		{
			name:    "finalized block",
			blockNr: "finalized",
			wantErr: false,
		},
		{
			name:    "pending block",
			blockNr: "pending",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getCode\",\"params\":[\"0x1234567890123456789023456789012345678901\",\"%s\"],\"id\":\"test\"}", tt.blockNr)
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
				_, ok := resObj["error"]
				require.False(t, ok)
				got := resObj["result"]
				require.Equal(t, wantKey, got)
			}
		})
	}
}

func TestGetStorageAt(t *testing.T) {
	hexValue := common.BytesToHash([]byte("value"))
	wantValue := "0x" + hex.EncodeToString(hexValue[:])
	tests := []struct {
		name    string
		blockNr string
		wantErr bool
	}{
		{
			name:    "latest block",
			blockNr: "latest",
			wantErr: false,
		},
		{
			name:    "safe block",
			blockNr: "safe",
			wantErr: false,
		},
		{
			name:    "finalized block",
			blockNr: "finalized",
			wantErr: false,
		},
		{
			name:    "pending block",
			blockNr: "pending",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hexKey := common.BytesToHash([]byte("key"))
			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getStorageAt\",\"params\":[\"0x1234567890123456789023456789012345678901\",\"%s\",\"%s\"],\"id\":\"test\"}", hexKey, tt.blockNr)
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
				_, ok := resObj["error"]
				require.False(t, ok)
				got := resObj["result"]
				require.Equal(t, wantValue, got)
			}
		})
	}
}

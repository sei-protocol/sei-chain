package evmrpc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/stretchr/testify/require"
)

func TestGetBalance(t *testing.T) {
	tests := []struct {
		name       string
		addr       string
		blockNr    string
		wantErr    bool
		wantAmount float64
	}{
		{
			name:       "latest block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "latest",
			wantErr:    false,
			wantAmount: 1000,
		},
		{
			name:       "safe block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "safe",
			wantErr:    false,
			wantAmount: 1000,
		},
		{
			name:       "finalized block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "finalized",
			wantErr:    false,
			wantAmount: 1000,
		},
		{
			name:       "pending block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "pending",
			wantErr:    false,
			wantAmount: 1000,
		},
		{
			name:       "evm address with sei address mapping",
			addr:       common.HexToAddress(common.Bytes2Hex([]byte("evmAddr"))).String(),
			blockNr:    "latest",
			wantErr:    false,
			wantAmount: 10,
		},
		{
			name:       "err: earliest block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "earliest",
			wantErr:    true,
			wantAmount: -1,
		},
		{
			name:       "err: numbered block",
			addr:       "0x1234567890123456789023456789012345678901",
			blockNr:    "0x1",
			wantErr:    true,
			wantAmount: -1,
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
				got := resObj["result"].(float64)
				require.Equal(t, tt.wantAmount, got)
			}
		})
	}
}

func TestGetCode(t *testing.T) {
	wantKey := base64.StdEncoding.EncodeToString([]byte("abc"))
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
		{
			name:    "err: earliest block",
			blockNr: "earliest",
			wantErr: true,
		},
		{
			name:    "err: numbered block",
			blockNr: "0x1",
			wantErr: true,
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
	wantValue := base64.StdEncoding.EncodeToString(hexValue[:])
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
		{
			name:    "err: earliest block",
			blockNr: "earliest",
			wantErr: true,
		},
		{
			name:    "err: numbered block",
			blockNr: "0x1",
			wantErr: true,
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

func TestGetProof(t *testing.T) {
	_, evmAddr := keeper.MockAddressPair()
	key, val := []byte("test"), []byte("abc")
	EVMKeeper.SetState(Ctx, evmAddr, common.BytesToHash(key), common.BytesToHash(val))
	// bump store version to be the latest block
	for i := 0; i < MockHeight; i++ {
		Ctx.MultiStore().(sdk.CommitMultiStore).Commit(true)
	}
	tests := []struct {
		key         string
		blockNr     string
		expectedVal []byte
	}{
		{
			key:         string(key),
			blockNr:     "latest",
			expectedVal: val,
		},
		{
			key:         string(key),
			blockNr:     "0x8",
			expectedVal: val,
		},
		{
			key:         "non existent",
			blockNr:     "latest",
			expectedVal: []byte{},
		},
	}
	for _, test := range tests {
		resObj := sendRequestGood(t, "getProof", evmAddr.Hex(), []interface{}{test.key}, test.blockNr)
		result := resObj["result"].(map[string]interface{})
		vals := result["hexValues"].([]interface{})
		require.Equal(t, common.BytesToHash(test.expectedVal), common.HexToHash(vals[0].(string)))
		proofs := result["storageProof"].([]interface{})
		require.Equal(t, "ics23:iavl", proofs[0].(map[string]interface{})["ops"].([]interface{})[0].(map[string]interface{})["type"].(string))
	}

	resObj := sendRequestBad(t, "getProof", evmAddr.Hex(), []interface{}{string("non existent")}, "latest")
	result := resObj["error"].(map[string]interface{})
	require.Equal(t, "error block", result["message"])
}

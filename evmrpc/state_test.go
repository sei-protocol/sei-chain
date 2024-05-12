package evmrpc_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestGetBalance(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
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
	Ctx = Ctx.WithBlockHeight(8)
}

func TestGetCode(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
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
	Ctx = Ctx.WithBlockHeight(8)
}

func TestGetStorageAt(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
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
	Ctx = Ctx.WithBlockHeight(8)
}

func TestGetProof(t *testing.T) {
	testApp := app.Setup(false, false)
	_, evmAddr := testkeeper.MockAddressPair()
	key, val := []byte("test"), []byte("abc")
	testApp.EvmKeeper.SetState(testApp.GetContextForDeliverTx([]byte{}), evmAddr, common.BytesToHash(key), common.BytesToHash(val))
	for i := 0; i < MockHeight; i++ {
		testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: int64(i + 1)})
		testApp.SetDeliverStateToCommit()
		_, err := testApp.Commit(context.Background())
		require.Nil(t, err)
	}
	stateAPI := evmrpc.NewStateAPI(&MockClient{}, &testApp.EvmKeeper, func(int64) sdk.Context { return testApp.GetCheckCtx() }, evmrpc.ConnectionTypeHTTP)
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000616263", testApp.EvmKeeper.GetState(testApp.GetCheckCtx(), evmAddr, common.BytesToHash(key)).Hex())
	tests := []struct {
		key         string
		blockNr     rpc.BlockNumber
		expectedVal []byte
	}{
		{
			key:         string(key),
			blockNr:     rpc.BlockNumber(-2),
			expectedVal: val,
		},
		{
			key:         string(key),
			blockNr:     rpc.BlockNumber(8),
			expectedVal: val,
		},
		{
			key:         "non existent",
			blockNr:     rpc.BlockNumber(-2),
			expectedVal: []byte{},
		},
	}
	for _, test := range tests {
		bptr := &rpc.BlockNumberOrHash{BlockNumber: &test.blockNr}
		res, err := stateAPI.GetProof(context.Background(), evmAddr, []string{test.key}, *bptr)
		require.Nil(t, err)
		vals := res.HexValues
		require.Equal(t, common.BytesToHash(test.expectedVal), common.HexToHash(vals[0]))
		proofs := res.StorageProof
		require.Equal(t, "ics23:iavl", proofs[0].Ops[0].Type)
	}
}

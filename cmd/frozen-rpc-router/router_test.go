package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRouteBlockParameters(t *testing.T) {
	r := newTestRouter(t)
	testCases := []struct {
		name     string
		method   string
		params   string
		wantHost string
	}{
		{name: "unclassified method", method: "eth_blockNumber", params: "[]", wantHost: "live:8545"},
		{name: "first interval genesis", method: "eth_getBlockByNumber", params: `["0x0",false]`, wantHost: "frozen-100:8545"},
		{name: "first interval upper edge", method: "eth_getBlockByNumber", params: `["0x63",false]`, wantHost: "frozen-100:8545"},
		{name: "second interval lower edge", method: "eth_getBlockByNumber", params: `["0x64",false]`, wantHost: "frozen-200:8545"},
		{name: "second interval upper edge", method: "debug_traceBlockByNumber", params: `["0xc7",{}]`, wantHost: "frozen-200:8545"},
		{name: "live interval lower edge", method: "eth_getBlockReceipts", params: `["0xc8"]`, wantHost: "live:8545"},
		{name: "latest tag", method: "eth_getBalance", params: `["0x0000000000000000000000000000000000000000","latest"]`, wantHost: "live:8545"},
		{name: "earliest tag", method: "eth_getCode", params: `["0x0000000000000000000000000000000000000000","earliest"]`, wantHost: "frozen-100:8545"},
		{name: "EIP-1898 number", method: "eth_call", params: `[{}, {"blockNumber":"0x64"}]`, wantHost: "frozen-200:8545"},
		{name: "EIP-1898 hash", method: "eth_call", params: `[{}, {"blockHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}]`, wantHost: "live:8545"},
		{name: "method-specific position", method: "eth_getStorageAt", params: `["0x0000000000000000000000000000000000000000","0x0","0x63"]`, wantHost: "frozen-100:8545"},
		{name: "optional block omitted", method: "eth_estimateGas", params: `[{}]`, wantHost: "live:8545"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			call := mustDecodeCall(t, testCase.method, testCase.params)
			target, routingErr := r.route(call)
			require.Nil(t, routingErr)
			require.Equal(t, testCase.wantHost, target.endpoint.Host)
		})
	}
}

func TestRouteRanges(t *testing.T) {
	r := newTestRouter(t)
	testCases := []struct {
		name      string
		method    string
		params    string
		wantHost  string
		wantError bool
	}{
		{name: "get logs defaults to latest", method: "eth_getLogs", params: `[{}]`, wantHost: "live:8545"},
		{name: "get logs only to block", method: "eth_getLogs", params: `[{"toBlock":"0x63"}]`, wantHost: "frozen-100:8545"},
		{name: "get logs within second interval", method: "eth_getLogs", params: `[{"fromBlock":"0x64","toBlock":"0xc7"}]`, wantHost: "frozen-200:8545"},
		{name: "get logs within live interval", method: "eth_getLogs", params: `[{"fromBlock":"0xc8","toBlock":"0xfa"}]`, wantHost: "live:8545"},
		{name: "get logs by hash", method: "eth_getLogs", params: `[{"blockHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}]`, wantHost: "live:8545"},
		{name: "get logs crosses frozen intervals", method: "eth_getLogs", params: `[{"fromBlock":"0x63","toBlock":"0x64"}]`, wantError: true},
		{name: "get logs crosses into live", method: "eth_getLogs", params: `[{"fromBlock":"0xc7","toBlock":"0xc8"}]`, wantError: true},
		{name: "fee history within interval", method: "eth_feeHistory", params: `["0x2","0x65",[]]`, wantHost: "frozen-200:8545"},
		{name: "fee history crosses intervals", method: "eth_feeHistory", params: `["0x2","0x64",[]]`, wantError: true},
		{name: "fee history crosses into live", method: "eth_feeHistory", params: `["0x2","0xc8",[]]`, wantError: true},
		{name: "fee history at latest", method: "eth_feeHistory", params: `["0x2","latest",[]]`, wantHost: "live:8545"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			call := mustDecodeCall(t, testCase.method, testCase.params)
			target, routingErr := r.route(call)
			if testCase.wantError {
				require.Nil(t, target)
				require.NotNil(t, routingErr)
				require.Equal(t, jsonRPCUnsupportedError, routingErr.Code)
				return
			}
			require.Nil(t, routingErr)
			require.Equal(t, testCase.wantHost, target.endpoint.Host)
		})
	}
}

func TestRouterForwardsSingleRequest(t *testing.T) {
	live := newRPCBackend(t, "live")
	frozen := newRPCBackend(t, "frozen")
	r, err := newRouter(live.server.URL, []frozenNodeConfig{{freezeHeight: 100, address: frozen.server.URL}}, live.server.Client(), defaultMaxRequestBodySize)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://router/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x63",false]}`))
	r.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "frozen", recorder.Header().Get("X-Upstream"))
	require.Equal(t, "frozen:100", recorder.Header().Get(rpcRouteHeader))
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":"frozen"}`, recorder.Body.String())
	require.EqualValues(t, 0, live.hits.Load())
	require.EqualValues(t, 1, frozen.hits.Load())
}

func TestRouterSplitsMixedBatch(t *testing.T) {
	live := newRPCBackend(t, "live")
	frozen100 := newRPCBackend(t, "frozen-100")
	frozen200 := newRPCBackend(t, "frozen-200")
	r, err := newRouter(live.server.URL, []frozenNodeConfig{
		{freezeHeight: 200, address: frozen200.server.URL},
		{freezeHeight: 100, address: frozen100.server.URL},
	}, live.server.Client(), defaultMaxRequestBodySize)
	require.NoError(t, err)

	body := `[
        {"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x63",false]},
        {"jsonrpc":"2.0","id":2,"method":"eth_getBlockByNumber","params":["0x64",false]},
        {"jsonrpc":"2.0","id":3,"method":"net_version","params":[]},
        {"jsonrpc":"2.0","id":4,"method":"eth_getLogs","params":[{"fromBlock":"0x63","toBlock":"0x64"}]},
        {"jsonrpc":"2.0","method":"eth_blockNumber","params":[]}
    ]`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://router/", strings.NewReader(body))
	r.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "mixed", recorder.Header().Get(rpcRouteHeader))
	var responses []struct {
		ID     json.RawMessage `json:"id"`
		Result string          `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responses))
	require.Len(t, responses, 4)
	byID := make(map[string]struct {
		result string
		err    *rpcError
	})
	for _, response := range responses {
		byID[string(response.ID)] = struct {
			result string
			err    *rpcError
		}{result: response.Result, err: response.Error}
	}
	require.Equal(t, "frozen-100", byID["1"].result)
	require.Equal(t, "frozen-200", byID["2"].result)
	require.Equal(t, "live", byID["3"].result)
	require.Equal(t, jsonRPCUnsupportedError, byID["4"].err.Code)
	require.EqualValues(t, 1, frozen100.hits.Load())
	require.EqualValues(t, 1, frozen200.hits.Load())
	require.EqualValues(t, 1, live.hits.Load())
}

func TestRouterOmitsErrorForUnsupportedNotification(t *testing.T) {
	r := newTestRouter(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://router/", strings.NewReader(`{"jsonrpc":"2.0","method":"eth_getLogs","params":[{"fromBlock":"0x63","toBlock":"0x64"}]}`))
	r.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Body.String())
}

func TestRouterRejectsOversizedRequest(t *testing.T) {
	r, err := newRouter("live:8545", nil, nil, 8)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://router/", bytes.NewReader([]byte("123456789")))
	r.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestNewRouterSortsAndValidatesFrozenNodes(t *testing.T) {
	r := newTestRouter(t)
	require.Equal(t, uint64(100), r.frozen[0].freezeHeight)
	require.Equal(t, uint64(200), r.frozen[1].freezeHeight)

	_, err := newRouter("live:8545", []frozenNodeConfig{
		{freezeHeight: 100, address: "one:8545"},
		{freezeHeight: 100, address: "two:8545"},
	}, nil, defaultMaxRequestBodySize)
	require.EqualError(t, err, "duplicate freeze height 100")
}

func newTestRouter(t *testing.T) *router {
	t.Helper()
	r, err := newRouter("live:8545", []frozenNodeConfig{
		{freezeHeight: 200, address: "frozen-200:8545"},
		{freezeHeight: 100, address: "frozen-100:8545"},
	}, nil, defaultMaxRequestBodySize)
	require.NoError(t, err)
	return r
}

func mustDecodeCall(t *testing.T, method, params string) rpcCall {
	t.Helper()
	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":` + strconv.Quote(method) + `,"params":` + params + `}`)
	call, err := decodeCall(raw)
	require.NoError(t, err)
	require.True(t, call.isValid)
	return call
}

type rpcBackend struct {
	server *httptest.Server
	hits   atomic.Int64
}

func newRPCBackend(t *testing.T, name string) *rpcBackend {
	t.Helper()
	backend := &rpcBackend{}
	backend.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		backend.hits.Add(1)
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", name)
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			var calls []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(trimmed, &calls))
			responses := make([]map[string]any, 0, len(calls))
			for _, call := range calls {
				id, hasID := call["id"]
				if !hasID {
					continue
				}
				responses = append(responses, map[string]any{"jsonrpc": "2.0", "id": id, "result": name})
			}
			if len(responses) != 0 {
				require.NoError(t, json.NewEncoder(w).Encode(responses))
			}
			return
		}
		var call map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(trimmed, &call))
		id, hasID := call["id"]
		if hasID {
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": name}))
		}
	}))
	t.Cleanup(backend.server.Close)
	return backend
}

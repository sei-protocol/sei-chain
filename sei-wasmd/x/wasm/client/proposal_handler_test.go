package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	"github.com/CosmWasm/wasmd/x/wasm/keeper"
)

func TestGovRestHandlers(t *testing.T) {
	type dict map[string]interface{}
	var (
		anyAddress = "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz"
		aBaseReq   = dict{
			"from":           anyAddress,
			"memo":           "rest test",
			"chain_id":       "testing",
			"account_number": "1",
			"sequence":       "1",
			"fees":           []dict{{"denom": "ustake", "amount": "1000000"}},
		}
	)
	encodingConfig := keeper.MakeEncodingConfig(t)
	clientCtx := client.Context{}.
		WithCodec(encodingConfig.Marshaler).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastBlock).
		WithChainID("testing")

	// router setup as in gov/client/rest/tx.go
	propSubRtr := mux.NewRouter().PathPrefix("/gov/proposals").Subrouter()
	for _, ph := range ProposalHandlers {
		r := ph.RESTHandler(clientCtx)
		propSubRtr.HandleFunc(fmt.Sprintf("/%s", r.SubRoute), r.Handler).Methods("POST")
	}

	specs := map[string]struct {
		srcBody dict
		srcPath string
		expCode int
	}{
		"store-code": {
			srcPath: "/gov/proposals/wasm_store_code",
			srcBody: dict{
				"title":          "Test Proposal",
				"description":    "My proposal",
				"type":           "store-code",
				"run_as":         "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"wasm_byte_code": []byte("valid wasm byte code"),
				"source":         "https://example.com/",
				"builder":        "my/builder:tag",
				"instantiate_permission": dict{
					"permission": "OnlyAddress",
					"address":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				},
				"deposit":  []dict{{"denom": "ustake", "amount": "10"}},
				"proposer": "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req": aBaseReq,
			},
			expCode: http.StatusOK,
		},
		"store-code without permission": {
			srcPath: "/gov/proposals/wasm_store_code",
			srcBody: dict{
				"title":          "Test Proposal",
				"description":    "My proposal",
				"type":           "store-code",
				"run_as":         "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"wasm_byte_code": []byte("valid wasm byte code"),
				"source":         "https://example.com/",
				"builder":        "my/builder:tag",
				"deposit":        []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":       "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":       aBaseReq,
			},
			expCode: http.StatusOK,
		},
		"store-code invalid permission": {
			srcPath: "/gov/proposals/wasm_store_code",
			srcBody: dict{
				"title":          "Test Proposal",
				"description":    "My proposal",
				"type":           "store-code",
				"run_as":         "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"wasm_byte_code": []byte("valid wasm byte code"),
				"source":         "https://example.com/",
				"builder":        "my/builder:tag",
				"instantiate_permission": dict{
					"permission": "Nobody",
					"address":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				},
				"deposit":  []dict{{"denom": "ustake", "amount": "10"}},
				"proposer": "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req": aBaseReq,
			},
			expCode: http.StatusBadRequest,
		},
		"store-code with incomplete proposal data: blank title": {
			srcPath: "/gov/proposals/wasm_store_code",
			srcBody: dict{
				"title":          "",
				"description":    "My proposal",
				"type":           "store-code",
				"run_as":         "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"wasm_byte_code": []byte("valid wasm byte code"),
				"source":         "https://example.com/",
				"builder":        "my/builder:tag",
				"instantiate_permission": dict{
					"permission": "OnlyAddress",
					"address":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				},
				"deposit":  []dict{{"denom": "ustake", "amount": "10"}},
				"proposer": "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req": aBaseReq,
			},
			expCode: http.StatusBadRequest,
		},
		"store-code with incomplete content data: no wasm_byte_code": {
			srcPath: "/gov/proposals/wasm_store_code",
			srcBody: dict{
				"title":          "Test Proposal",
				"description":    "My proposal",
				"type":           "store-code",
				"run_as":         "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"wasm_byte_code": "",
				"source":         "https://example.com/",
				"builder":        "my/builder:tag",
				"instantiate_permission": dict{
					"permission": "OnlyAddress",
					"address":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				},
				"deposit":  []dict{{"denom": "ustake", "amount": "10"}},
				"proposer": "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req": aBaseReq,
			},
			expCode: http.StatusBadRequest,
		},
		"instantiate contract": {
			srcPath: "/gov/proposals/wasm_instantiate",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "instantiate",
				"run_as":      "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"admin":       "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"code_id":     "1",
				"label":       "https://example.com/",
				"msg":         dict{"recipient": "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz"},
				"funds":       []dict{{"denom": "ustake", "amount": "100"}},
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusOK,
		},
		"migrate contract": {
			srcPath: "/gov/proposals/wasm_migrate",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "migrate",
				"contract":    "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
				"code_id":     "1",
				"msg":         dict{"foo": "bar"},
				"run_as":      "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusOK,
		},
		"execute contract": {
			srcPath: "/gov/proposals/wasm_execute",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "migrate",
				"contract":    "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
				"msg":         dict{"foo": "bar"},
				"run_as":      "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusOK,
		},
		"execute contract fails with no run_as": {
			srcPath: "/gov/proposals/wasm_execute",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "migrate",
				"contract":    "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
				"msg":         dict{"foo": "bar"},
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusBadRequest,
		},
		"execute contract fails with no message": {
			srcPath: "/gov/proposals/wasm_execute",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "migrate",
				"contract":    "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
				"run_as":      "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusBadRequest,
		},
		"sudo contract": {
			srcPath: "/gov/proposals/wasm_sudo",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "migrate",
				"contract":    "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
				"msg":         dict{"foo": "bar"},
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusOK,
		},
		"sudo contract fails with no message": {
			srcPath: "/gov/proposals/wasm_sudo",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "migrate",
				"contract":    "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusBadRequest,
		},
		"update contract admin": {
			srcPath: "/gov/proposals/wasm_update_admin",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "migrate",
				"contract":    "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
				"new_admin":   "cosmos100dejzacpanrldpjjwksjm62shqhyss44jf5xz",
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusOK,
		},
		"clear contract admin": {
			srcPath: "/gov/proposals/wasm_clear_admin",
			srcBody: dict{
				"title":       "Test Proposal",
				"description": "My proposal",
				"type":        "migrate",
				"contract":    "cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr",
				"deposit":     []dict{{"denom": "ustake", "amount": "10"}},
				"proposer":    "cosmos1ve557a5g9yw2g2z57js3pdmcvd5my6g8ze20np",
				"base_req":    aBaseReq,
			},
			expCode: http.StatusOK,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			src, err := json.Marshal(spec.srcBody)
			require.NoError(t, err)

			// when
			r := httptest.NewRequest("POST", spec.srcPath, bytes.NewReader(src))
			w := httptest.NewRecorder()
			propSubRtr.ServeHTTP(w, r)

			// then
			require.Equal(t, spec.expCode, w.Code, w.Body.String())
		})
	}
}

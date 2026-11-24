package app_test

import (
	"fmt"
	"os"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/stretchr/testify/require"
)

// TestMsgExecuteContractDisabledInDeliverTx delivers a CW20 transfer as a MsgExecuteContract
// with the wasm msgExecuteContractDisabled and contractExecutionDisabled params off and each on.
func TestMsgExecuteContractDisabledInDeliverTx(t *testing.T) {
	specs := map[string]struct {
		msgExecuteContractDisabled bool
		contractExecutionDisabled  bool
		expCode                    uint32
		expCodespace               string
		expReceived                string
	}{
		"params off": {expReceived: "100"},
		"msgExecuteContractDisabled on": {
			msgExecuteContractDisabled: true,
			expCode:                    wasmtypes.ErrMsgExecuteContractDisabled.ABCICode(),
			expCodespace:               wasmtypes.ModuleName,
			expReceived:                "0",
		},
		"contractExecutionDisabled on": {
			contractExecutionDisabled: true,
			expCode:                   wasmtypes.ErrContractExecutionDisabled.ABCICode(),
			expCodespace:              wasmtypes.ModuleName,
			expReceived:               "0",
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			a := processblock.NewTestApp(t)
			p := processblock.CommonPreset(a)
			holder := p.Admin
			recipient := a.NewAccount()
			cw20 := instantiateCW20(t, a, holder)
			params := a.WasmKeeper.GetParams(a.Ctx())
			params.MsgExecuteContractDisabled = spec.msgExecuteContractDisabled
			params.ContractExecutionDisabled = spec.contractExecutionDisabled
			a.WasmKeeper.SetParams(a.Ctx(), params)

			transfer := &wasmtypes.MsgExecuteContract{
				Sender:   holder.String(),
				Contract: cw20.String(),
				Msg:      []byte(fmt.Sprintf(`{"transfer":{"recipient":"%s","amount":"100"}}`, recipient)),
			}
			results := a.RunBlockDetailed([]signing.Tx{p.AdminSign(a, transfer)})

			require.Len(t, results, 1)
			require.Equal(t, spec.expCode, results[0].Code, results[0].Log)
			require.Equal(t, spec.expCodespace, results[0].Codespace)
			balance, err := a.WasmKeeper.QuerySmart(a.Ctx(), cw20, []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, recipient)))
			require.NoError(t, err)
			require.JSONEq(t, fmt.Sprintf(`{"balance":"%s"}`, spec.expReceived), string(balance))
		})
	}
}

// instantiateCW20 deploys a CW20 whose whole supply belongs to holder.
func instantiateCW20(t *testing.T, a *processblock.App, holder sdk.AccAddress) sdk.AccAddress {
	t.Helper()
	code, err := os.ReadFile("../contracts/wasm/cw20_base.wasm")
	require.NoError(t, err)
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&a.WasmKeeper)
	codeID, err := contractKeeper.Create(a.Ctx(), holder, code, nil)
	require.NoError(t, err)
	initMsg := fmt.Sprintf(`{"name":"test","symbol":"TEST","decimals":6,"initial_balances":[{"address":"%s","amount":"1000"}]}`, holder)
	cw20, _, err := contractKeeper.Instantiate(a.Ctx(), codeID, holder, holder, []byte(initMsg), "cw20", sdk.NewCoins())
	require.NoError(t, err)
	return cw20
}

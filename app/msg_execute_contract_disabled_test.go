package app_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/stretchr/testify/require"
)

// TestMsgExecuteContractDisabledInDeliverTx delivers a CW20 transfer as a MsgExecuteContract
// sent directly and as one wrapped in an authz MsgExec, with the wasm
// msgExecuteContractDisabled and contractExecutionDisabled params off and each on.
func TestMsgExecuteContractDisabledInDeliverTx(t *testing.T) {
	specs := map[string]struct {
		msgExecuteContractDisabled bool
		contractExecutionDisabled  bool
		expCode                    uint32
		expCodespace               string
		expReceived                string
	}{
		"params off": {expReceived: "200"},
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
			granter := p.Admin
			grantee := a.NewSignableAccount("grantee")
			a.FundAccount(grantee, 100000000)
			recipient := a.NewAccount()
			cw20 := instantiateCW20(t, a, granter)
			executeContract := authz.NewGenericAuthorization(sdk.MsgTypeURL(&wasmtypes.MsgExecuteContract{}))
			require.NoError(t, a.AuthzKeeper.SaveGrant(a.Ctx(), grantee, granter, executeContract, time.Now().Add(time.Hour)))
			params := a.WasmKeeper.GetParams(a.Ctx())
			params.MsgExecuteContractDisabled = spec.msgExecuteContractDisabled
			params.ContractExecutionDisabled = spec.contractExecutionDisabled
			a.WasmKeeper.SetParams(a.Ctx(), params)

			transfer := &wasmtypes.MsgExecuteContract{
				Sender:   granter.String(),
				Contract: cw20.String(),
				Msg:      []byte(fmt.Sprintf(`{"transfer":{"recipient":"%s","amount":"100"}}`, recipient)),
			}
			exec := authz.NewMsgExec(grantee, []sdk.Msg{transfer})
			results := a.RunBlockDetailed([]signing.Tx{
				p.AdminSign(a, transfer),
				a.Sign(grantee, 10000000, &exec),
			})

			require.Len(t, results, 2)
			for _, result := range results {
				require.Equal(t, spec.expCode, result.Code, result.Log)
				require.Equal(t, spec.expCodespace, result.Codespace)
			}
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

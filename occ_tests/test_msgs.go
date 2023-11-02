package occ_tests

import (
	"fmt"
	"github.com/CosmWasm/wasmd/x/wasm"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

const instantiateMsg = `{"whitelist": ["sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag"],
    "use_whitelist":false,"admin":"sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag",
	"limit_order_fee":{"decimal":"0.0001","negative":false},
	"market_order_fee":{"decimal":"0.0001","negative":false},
	"liquidation_order_fee":{"decimal":"0.0001","negative":false},
	"margin_ratio":{"decimal":"0.0625","negative":false},
	"max_leverage":{"decimal":"4","negative":false},
	"default_base":"USDC",
	"native_token":"USDC","denoms": ["SEI","ATOM","USDC","SOL","ETH","OSMO","AVAX","BTC"],
	"full_denom_mapping": [["usei","SEI","0.000001"],["uatom","ATOM","0.000001"],["uusdc","USDC","0.000001"]],
	"funding_payment_lookback":3600,"spot_market_contract":"sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag",
	"supported_collateral_denoms": ["USDC"],
	"supported_multicollateral_denoms": ["ATOM"],
	"oracle_denom_mapping": [["usei","SEI","1"],["uatom","ATOM","1"],["uusdc","USDC","1"],["ueth","ETH","1"]],
	"multicollateral_whitelist": ["sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag"],
	"multicollateral_whitelist_enable": true,
	"funding_payment_pairs": [["USDC","ETH"]],
	"default_margin_ratios":{
		"initial":"0.3",
		"partial":"0.25",
		"maintenance":"0.06"
	}}`

func wasmInstantiate(tCtx *TestContext, count int) []sdk.Msg {
	var msgs []sdk.Msg
	for i := 0; i < count; i++ {
		msgs = append(msgs, &wasm.MsgInstantiateContract{
			Sender: tCtx.Signer.Sender.String(),
			Admin:  tCtx.TestAccount1.String(),
			CodeID: tCtx.CodeID,
			Label:  fmt.Sprintf("test-%d", i),
			Msg:    []byte(instantiateMsg),
			Funds:  funds(100000),
		})
	}
	return msgs
}

func bankTransfer(tCtx *TestContext, count int) []sdk.Msg {
	var msgs []sdk.Msg
	for i := 0; i < count; i++ {
		msgs = append(msgs, banktypes.NewMsgSend(tCtx.Signer.Sender, tCtx.TestAccount2, funds(int64(i+1))))
	}
	return msgs
}

func governanceSubmitProposal(tCtx *TestContext, count int) []sdk.Msg {
	var msgs []sdk.Msg
	for i := 0; i < count; i++ {
		content := govtypes.NewTextProposal(fmt.Sprintf("Proposal %d", i), "test", true)
		mp, err := govtypes.NewMsgSubmitProposalWithExpedite(content, funds(10000), tCtx.Signer.Sender, true)
		if err != nil {
			panic(err)
		}
		msgs = append(msgs, mp)
	}
	return msgs
}

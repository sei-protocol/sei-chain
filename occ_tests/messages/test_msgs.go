package messages

import (
	"fmt"

	"github.com/CosmWasm/wasmd/x/wasm"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/sei-protocol/sei-chain/occ_tests/utils"
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

func WasmInstantiate(tCtx *utils.TestContext, count int) []*utils.TestMessage {
	var msgs []*utils.TestMessage
	for i := 0; i < count; i++ {
		msgs = append(msgs, &utils.TestMessage{
			Msg: &wasm.MsgInstantiateContract{
				Sender: tCtx.TestAccounts[0].AccountAddress.String(),
				Admin:  tCtx.TestAccounts[1].AccountAddress.String(),
				CodeID: tCtx.CodeID,
				Label:  fmt.Sprintf("test-%d", i),
				Msg:    []byte(instantiateMsg),
				Funds:  utils.Funds(100000),
			},
			Type: "WasmInstantitate",
		})
	}
	return msgs
}

func EVMTransfer(tCtx *utils.TestContext, count int) []sdk.Msg {
	var msgs []sdk.Msg
	for i := 0; i < count; i++ {
		// generate new evm account
		// fund the account
		// generate transfer 0 funds to himself
		// build message and append to result
	}
	return msgs
}

func BankTransfer(tCtx *utils.TestContext, count int) []*utils.TestMessage {
	var msgs []*utils.TestMessage
	for i := 0; i < count; i++ {
		msg := banktypes.NewMsgSend(tCtx.TestAccounts[0].AccountAddress, tCtx.TestAccounts[1].AccountAddress, utils.Funds(int64(i+1)))
		msgs = append(msgs, &utils.TestMessage{Msg: msg, Type: "BankTransfer"})
	}
	return msgs
}

func GovernanceSubmitProposal(tCtx *utils.TestContext, count int) []*utils.TestMessage {
	var msgs []*utils.TestMessage
	for i := 0; i < count; i++ {
		content := govtypes.NewTextProposal(fmt.Sprintf("Proposal %d", i), "test", true)
		mp, err := govtypes.NewMsgSubmitProposalWithExpedite(content, utils.Funds(10000), tCtx.TestAccounts[0].AccountAddress, true)
		if err != nil {
			panic(err)
		}
		msgs = append(msgs, &utils.TestMessage{Msg: mp, Type: "GovernanceSubmitProposal"})
	}
	return msgs
}

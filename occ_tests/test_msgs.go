package occ_tests

import (
	"fmt"
	"github.com/CosmWasm/wasmd/x/wasm"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

func wasmInstantiate(tCtx *TestContext, count int) []sdk.Msg {
	var msgs []sdk.Msg
	for i := 0; i < count; i++ {
		msgs = append(msgs, &wasm.MsgInstantiateContract{
			Sender: tCtx.Signer.Sender.String(),
			Admin:  tCtx.TestAccount1.String(),
			CodeID: tCtx.CodeID,
			Label:  fmt.Sprintf("test-%d", i),
			Msg:    []byte(INSTANTIATE),
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

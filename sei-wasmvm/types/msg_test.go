package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWasmMsgInstantiateSerialization(t *testing.T) {
	// no admin
	document := []byte(`{"instantiate":{"admin":null,"code_id":7897,"msg":"eyJjbGFpbSI6e319","funds":[{"denom":"stones","amount":"321"}],"label":"my instance"}}`)

	var msg WasmMsg
	err := json.Unmarshal(document, &msg)
	require.NoError(t, err)

	require.Nil(t, msg.Instantiate2)
	require.Nil(t, msg.Execute)
	require.Nil(t, msg.Migrate)
	require.Nil(t, msg.UpdateAdmin)
	require.Nil(t, msg.ClearAdmin)
	require.NotNil(t, msg.Instantiate)

	require.Equal(t, "", msg.Instantiate.Admin)
	require.Equal(t, uint64(7897), msg.Instantiate.CodeID)
	require.Equal(t, []byte(`{"claim":{}}`), msg.Instantiate.Msg)
	require.Equal(t, Coins{
		{"stones", "321"},
	}, msg.Instantiate.Funds)
	require.Equal(t, "my instance", msg.Instantiate.Label)

	// admin
	document2 := []byte(`{"instantiate":{"admin":"king","code_id":7897,"msg":"eyJjbGFpbSI6e319","funds":[],"label":"my instance"}}`)

	err2 := json.Unmarshal(document2, &msg)
	require.NoError(t, err2)

	require.Nil(t, msg.Instantiate2)
	require.Nil(t, msg.Execute)
	require.Nil(t, msg.Migrate)
	require.Nil(t, msg.UpdateAdmin)
	require.Nil(t, msg.ClearAdmin)
	require.NotNil(t, msg.Instantiate)

	require.Equal(t, "king", msg.Instantiate.Admin)
	require.Equal(t, uint64(7897), msg.Instantiate.CodeID)
	require.Equal(t, []byte(`{"claim":{}}`), msg.Instantiate.Msg)
	require.Equal(t, Coins{
		{"stones", "321"},
	}, msg.Instantiate.Funds)
	require.Equal(t, "my instance", msg.Instantiate.Label)
}

func TestWasmMsgInstantiate2Serialization(t *testing.T) {
	document := []byte(`{"instantiate2":{"admin":null,"code_id":7897,"label":"my instance","msg":"eyJjbGFpbSI6e319","funds":[{"denom":"stones","amount":"321"}],"salt":"UkOVazhiwoo="}}`)

	var msg WasmMsg
	err := json.Unmarshal(document, &msg)
	require.NoError(t, err)

	require.Nil(t, msg.Instantiate)
	require.Nil(t, msg.Execute)
	require.Nil(t, msg.Migrate)
	require.Nil(t, msg.UpdateAdmin)
	require.Nil(t, msg.ClearAdmin)
	require.NotNil(t, msg.Instantiate2)

	require.Equal(t, "", msg.Instantiate2.Admin)
	require.Equal(t, uint64(7897), msg.Instantiate2.CodeID)
	require.Equal(t, []byte(`{"claim":{}}`), msg.Instantiate2.Msg)
	require.Equal(t, Coins{
		{"stones", "321"},
	}, msg.Instantiate2.Funds)
	require.Equal(t, "my instance", msg.Instantiate2.Label)
	require.Equal(t, []byte{0x52, 0x43, 0x95, 0x6b, 0x38, 0x62, 0xc2, 0x8a}, msg.Instantiate2.Salt)
}

func TestGovMsgVoteSerialization(t *testing.T) {
	document := []byte(`{"vote":{"proposal_id":4,"vote":"no_with_veto"}}`)

	var msg GovMsg
	err := json.Unmarshal(document, &msg)
	require.NoError(t, err)

	require.Nil(t, msg.VoteWeighted)
	require.NotNil(t, msg.Vote)

	require.Equal(t, uint64(4), msg.Vote.ProposalId)
	require.Equal(t, NoWithVeto, msg.Vote.Vote)
}

func TestGovMsgVoteWeightedSerialization(t *testing.T) {
	document := []byte(`{"vote_weighted":{"proposal_id":25,"options":[{"option":"yes","weight":"0.25"},{"option":"no","weight":"0.25"},{"option":"abstain","weight":"0.5"}]}}`)

	var msg GovMsg
	err := json.Unmarshal(document, &msg)
	require.NoError(t, err)

	require.Nil(t, msg.Vote)
	require.NotNil(t, msg.VoteWeighted)

	require.Equal(t, uint64(25), msg.VoteWeighted.ProposalId)
	require.Equal(t, []WeightedVoteOption{
		{Yes, "0.25"},
		{No, "0.25"},
		{Abstain, "0.5"},
	}, msg.VoteWeighted.Options)
}

func TestMsgFundCommunityPoolSerialization(t *testing.T) {
	document := []byte(`{"fund_community_pool":{"amount":[{"amount":"300","denom":"adenom"},{"amount":"400","denom":"bdenom"}]}}`)

	var msg DistributionMsg
	err := json.Unmarshal(document, &msg)
	require.NoError(t, err)

	require.Equal(t, Coins{{"adenom", "300"}, {"bdenom", "400"}}, msg.FundCommunityPool.Amount)
}

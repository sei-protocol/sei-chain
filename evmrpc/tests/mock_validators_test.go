package tests

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockValidatorsKeepPagesForOneHeight(t *testing.T) {
	const ed25519Key = `{"type":"tendermint/PubKeyEd25519","value":"P3MLnMNN/76GwQiCDHiniyNhRJ2sMPvtKxmvS8l4MyI="}`
	raw := json.RawMessage(`{"traces":[
		{"endpoint":"Validators","arguments":["10","1"],"response":{"block_height":"10","count":"1","total":"2","validators":[{"pub_key":` + ed25519Key + `,"voting_power":"1","proposer_priority":"0"}]}},
		{"endpoint":"Validators","arguments":["10","2"],"response":{"block_height":"10","count":"1","total":"2","validators":[{"pub_key":` + ed25519Key + `,"voting_power":"2","proposer_priority":"0"}]}}
	]}`)
	client := &MockClient{}
	mockTendermintStateFromJson(raw, client)

	height := int64(10)
	page1, page2 := 1, 2
	first, err := client.Validators(t.Context(), &height, &page1, nil)
	require.NoError(t, err)
	second, err := client.Validators(t.Context(), &height, &page2, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Validators[0].VotingPower)
	require.Equal(t, int64(2), second.Validators[0].VotingPower)
}

func TestMockValidatorsCompletesARecordingThatStopsEarly(t *testing.T) {
	raw := json.RawMessage(`{"traces":[
		{"endpoint":"Validators","arguments":["10"],"response":{"block_height":"10","count":"1","total":"2","validators":[{"pub_key":{"type":"tendermint/PubKeyEd25519","value":"P3MLnMNN/76GwQiCDHiniyNhRJ2sMPvtKxmvS8l4MyI="},"voting_power":"1","proposer_priority":"0"}]}}
	]}`)
	client := &MockClient{}
	mockTendermintStateFromJson(raw, client)

	height := int64(10)
	page := 2
	res, err := client.Validators(t.Context(), &height, &page, nil)
	require.NoError(t, err)
	require.Empty(t, res.Validators)
	require.Equal(t, 1, res.Total)
}

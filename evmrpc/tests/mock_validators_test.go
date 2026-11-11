package tests

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const mockValidatorPubKey = `{"type":"tendermint/PubKeyEd25519","value":"P3MLnMNN/76GwQiCDHiniyNhRJ2sMPvtKxmvS8l4MyI="}`

func TestMockValidatorsKeepPagesForOneHeight(t *testing.T) {
	raw := json.RawMessage(`{"traces":[
		{"endpoint":"Validators","arguments":["10","1","1"],"response":{"block_height":"10","count":"1","total":"2","validators":[{"pub_key":` + mockValidatorPubKey + `,"voting_power":"1","proposer_priority":"0"}]}},
		{"endpoint":"Validators","arguments":["10","2","1"],"response":{"block_height":"10","count":"1","total":"2","validators":[{"pub_key":` + mockValidatorPubKey + `,"voting_power":"2","proposer_priority":"0"}]}}
	]}`)
	client := &MockClient{}
	mockTendermintStateFromJson(raw, client)

	height := int64(10)
	page1, page2, perPage := 1, 2, 1
	first, err := client.Validators(t.Context(), &height, &page1, &perPage)
	require.NoError(t, err)
	second, err := client.Validators(t.Context(), &height, &page2, &perPage)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Validators[0].VotingPower)
	require.Equal(t, int64(2), second.Validators[0].VotingPower)
}

func TestMockValidatorsServeOnlyTheRecordedPageSize(t *testing.T) {
	raw := json.RawMessage(`{"traces":[
		{"endpoint":"Validators","arguments":["10","1","100"],"response":{"block_height":"10","count":"1","total":"1","validators":[{"pub_key":` + mockValidatorPubKey + `,"voting_power":"1","proposer_priority":"0"}]}}
	]}`)
	client := &MockClient{}
	mockTendermintStateFromJson(raw, client)

	height := int64(10)
	page1, page2, recorded, other := 1, 2, 100, 30
	res, err := client.Validators(t.Context(), &height, &page1, &recorded)
	require.NoError(t, err)
	require.Len(t, res.Validators, 1)

	_, err = client.Validators(t.Context(), &height, &page1, &other)
	require.Error(t, err)
	_, err = client.Validators(t.Context(), &height, &page1, nil)
	require.Error(t, err)
	_, err = client.Validators(t.Context(), &height, &page2, &recorded)
	require.Error(t, err)
}

func TestMockValidatorsPageAnUnsizedRecordingAtTheRequestedSize(t *testing.T) {
	raw := json.RawMessage(`{"traces":[
		{"endpoint":"Validators","arguments":["10","1"],"response":{"block_height":"10","count":"1","total":"2","validators":[{"pub_key":` + mockValidatorPubKey + `,"voting_power":"1","proposer_priority":"0"}]}},
		{"endpoint":"Validators","arguments":["10","2"],"response":{"block_height":"10","count":"1","total":"2","validators":[{"pub_key":` + mockValidatorPubKey + `,"voting_power":"2","proposer_priority":"0"}]}}
	]}`)
	client := &MockClient{}
	mockTendermintStateFromJson(raw, client)

	height := int64(10)
	page1, page2, perPage := 1, 2, 100
	first, err := client.Validators(t.Context(), &height, &page1, &perPage)
	require.NoError(t, err)
	require.Equal(t, 2, first.Total)
	require.Equal(t, 2, first.Count)
	require.Len(t, first.Validators, 2)
	require.Equal(t, int64(1), first.Validators[0].VotingPower)
	require.Equal(t, int64(2), first.Validators[1].VotingPower)

	second, err := client.Validators(t.Context(), &height, &page2, &perPage)
	require.NoError(t, err)
	require.Empty(t, second.Validators)
	require.Equal(t, 2, second.Total)
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

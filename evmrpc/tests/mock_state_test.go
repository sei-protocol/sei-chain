package tests

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockTendermintStateLoadsARecordingThatStartsWithStatus(t *testing.T) {
	raw := json.RawMessage(`{"traces":[
		{"endpoint":"Status","arguments":[],"response":{}},
		{"endpoint":"Block","arguments":["10"],"response":{}},
		{"endpoint":"Validators","arguments":["9","1","100"],"response":{"block_height":"9","count":"0","total":"0","validators":[]}}
	]}`)

	require.NotPanics(t, func() { mockTendermintStateFromJson(raw, &MockClient{}) })
	require.Equal(t, int64(10), recordedBlockHeight(raw))
}

func TestRecordedBlockHeightRequiresABlockTrace(t *testing.T) {
	raw := json.RawMessage(`{"traces":[
		{"endpoint":"Status","arguments":[],"response":{}},
		{"endpoint":"Validators","arguments":["9","1","100"],"response":{"block_height":"9","count":"0","total":"0","validators":[]}}
	]}`)

	require.PanicsWithValue(t, "recording has no Block trace", func() { recordedBlockHeight(raw) })
}

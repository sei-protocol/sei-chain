package types

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/internal/jsontypes"
)

// Verify that the event data types satisfy their shared interface.
var (
	_ EventData = EventDataBlockSyncStatus{}
	_ EventData = EventDataCompleteProposal{}
	_ EventData = EventDataNewBlock{}
	_ EventData = EventDataNewBlockHeader{}
	_ EventData = EventDataNewEvidence{}
	_ EventData = EventDataNewRound{}
	_ EventData = EventDataRoundState{}
	_ EventData = EventDataStateSyncStatus{}
	_ EventData = EventDataTx{}
	_ EventData = EventDataValidatorSetUpdates{}
	_ EventData = EventDataVote{}
	_ EventData = EventDataString("")
)

func TestQueryTxFor(t *testing.T) {
	tx := Tx("foo")
	assert.Equal(t,
		fmt.Sprintf("tm.event = 'Tx' AND tx.hash = '%X'", tx.Hash()),
		EventQueryTxFor(tx).String(),
	)
}

func TestQueryForEvent(t *testing.T) {
	assert.Equal(t,
		"tm.event = 'NewBlock'",
		QueryForEvent(EventNewBlockValue).String(),
	)
	assert.Equal(t,
		"tm.event = 'NewEvidence'",
		QueryForEvent(EventNewEvidenceValue).String(),
	)
}

func TestTryUnmarshalForEvent(t *testing.T) {
	eventData := EventDataTx{
		TxResult: types.TxResult{
			Height: 123,
		},
	}
	garbage := json.RawMessage("stuff")

	bz, err := jsontypes.Marshal(eventData)
	require.NoError(t, err)

	unmarshaled, err := TryUnmarshalEventData(json.RawMessage(bz))
	require.NoError(t, err)
	assert.Equal(t, eventData, unmarshaled)

	_, err = TryUnmarshalEventData(garbage)
	require.Error(t, err)
}

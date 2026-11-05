package evmonlyapp

import (
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEpochWindowMiscChangesRoundTrip(t *testing.T) {
	for _, window := range []epoch.Window{
		evmOnlyTestEpochWindow(),
		evmOnlyTestShiftedEpochWindow(utils.None[*atypes.Epoch]()),
	} {
		rows := epochWindowTestRows(window)
		got, err := decodeEpochWindowRows(
			rows[epochWindowPrevKey], rows[epochWindowPrevKey] != nil,
			rows[epochWindowCurrentKey], rows[epochWindowCurrentKey] != nil,
			rows[epochWindowNextKey], rows[epochWindowNextKey] != nil,
		)
		require.NoError(t, err)
		require.True(t, got.OrPanic("epoch window").Equal(window))
	}
}

func TestEpochWindowRowsRejectPartialState(t *testing.T) {
	rows := epochWindowTestRows(evmOnlyTestEpochWindow())
	for _, tc := range []struct {
		name    string
		prev    bool
		current bool
		next    bool
	}{
		{name: "prev only", prev: true},
		{name: "current only", current: true},
		{name: "next only", next: true},
		{name: "prev and current", prev: true, current: true},
		{name: "prev and next", prev: true, next: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeEpochWindowRows(
				rows[epochWindowPrevKey], tc.prev,
				rows[epochWindowCurrentKey], tc.current,
				rows[epochWindowNextKey], tc.next,
			)
			require.Error(t, err)
		})
	}
}

func epochWindowTestRows(window epoch.Window) map[string][]byte {
	rows := map[string][]byte{}
	for _, change := range epochWindowMiscChanges(window) {
		rows[string(change.Key)] = change.Value
	}
	return rows
}

package evmonlyapp

import (
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEpochWindowMiscChangesRoundTrip(t *testing.T) {
	window := evmOnlyTestEpochWindow()
	rows := epochWindowTestRows(t, window)
	got, err := decodeEpochWindowRows(rows(epochWindowPrevKey), rows(epochWindowCurrentKey), rows(epochWindowNextKey))
	require.NoError(t, err)
	require.True(t, got.OrPanic("epoch window").Equal(window))

	got, err = decodeEpochWindowRows(utils.None[[]byte](), utils.None[[]byte](), utils.None[[]byte]())
	require.NoError(t, err)
	require.False(t, got.IsPresent())
}

func TestEpochWindowMiscChangesRequirePrev(t *testing.T) {
	_, err := epochWindowMiscChanges(evmOnlyTestShiftedEpochWindow(utils.None[*atypes.Epoch]()))
	require.Error(t, err)
}

func TestEpochWindowRowsRejectPartialState(t *testing.T) {
	rows := epochWindowTestRows(t, evmOnlyTestEpochWindow())
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
		{name: "current and next", current: true, next: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			present := func(key string, ok bool) utils.Option[[]byte] {
				if !ok {
					return utils.None[[]byte]()
				}
				return rows(key)
			}
			_, err := decodeEpochWindowRows(
				present(epochWindowPrevKey, tc.prev),
				present(epochWindowCurrentKey, tc.current),
				present(epochWindowNextKey, tc.next),
			)
			require.Error(t, err)
		})
	}
}

// epochWindowTestRows returns a lookup of the rows that commit window, by key.
func epochWindowTestRows(t *testing.T, window epoch.Window) func(key string) utils.Option[[]byte] {
	t.Helper()
	changes, err := epochWindowMiscChanges(window)
	require.NoError(t, err)
	rows := map[string][]byte{}
	for _, change := range changes {
		rows[string(change.Key)] = change.Value
	}
	return func(key string) utils.Option[[]byte] {
		if raw, ok := rows[key]; ok {
			return utils.Some(raw)
		}
		return utils.None[[]byte]()
	}
}

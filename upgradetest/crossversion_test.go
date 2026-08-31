package upgradetest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCrossVersionArtifactRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.json")
	before := &CrossVersion{
		node:         "sei-node-0",
		artifactPath: path,
		values:       map[string]json.RawMessage{},
	}
	before.Record(t, "modules", []string{"feegrant", "ibc"})
	before.save(t)

	after := &CrossVersion{
		node:         "sei-node-0",
		artifactPath: path,
		values:       map[string]json.RawMessage{},
	}
	after.load(t)
	var modules []string
	after.Replay(t, "modules", &modules)
	require.Equal(t, []string{"feegrant", "ibc"}, modules)
}

func TestExtractGenesisSkipsLogsAndReadsPrettyJSON(t *testing.T) {
	genesis, err := extractGenesis([]byte(`starting export
{"level":"info","message":"loading"}
{
  "app_state": {
    "bank": {"params": {}}
  }
}
finished
`))
	require.NoError(t, err)
	require.Contains(t, genesis.AppState, "bank")
}

func TestExtractGenesisRejectsOutputWithoutAppState(t *testing.T) {
	_, err := extractGenesis([]byte(`{"level":"info","message":"loading"}`))
	require.ErrorContains(t, err, "no genesis document")
}

func TestParseJSONIntAcceptsNumbersAndStrings(t *testing.T) {
	for _, encoded := range []string{"67", `"67"`} {
		value, err := parseJSONInt(json.RawMessage(encoded))
		require.NoError(t, err)
		require.Equal(t, int64(67), value)
	}

	_, err := parseJSONInt(json.RawMessage(`"not-a-height"`))
	require.Error(t, err)
}

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

func TestParseABCIQueryResponse(t *testing.T) {
	value, code, log, err := parseABCIQueryResponse([]byte(`{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "response": {
      "code": 0,
      "log": "",
      "value": "aGVsbG8="
    }
  }
}`))
	require.NoError(t, err)
	require.Equal(t, uint32(0), code)
	require.Equal(t, []byte("hello"), value)
	require.Empty(t, log)

	value, code, log, err = parseABCIQueryResponse([]byte(`{
  "result": {
    "response": {
      "code": "0",
      "value": null
    }
  }
}`))
	require.NoError(t, err)
	require.Equal(t, uint32(0), code)
	require.Empty(t, value)
	require.Empty(t, log)
}

func TestParseBlockAppHash(t *testing.T) {
	hash, err := parseBlockAppHash([]byte(`{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "block": {
      "header": {
        "app_hash": "0a0b0c0d"
      }
    }
  }
}`))
	require.NoError(t, err)
	require.Equal(t, []byte{0x0a, 0x0b, 0x0c, 0x0d}, hash)

	_, err = parseBlockAppHash([]byte(`{"result":{"block":{"header":{"app_hash":""}}}}`))
	require.ErrorContains(t, err, "no app_hash")
}

func TestSeidSignalScript(t *testing.T) {
	script := seidSignalScript("KILL")
	require.Contains(t, script, `kill -KILL "$pid"`)
	require.Contains(t, script, `${comm%/comm}`)
}

func TestSeidRestartLogPath(t *testing.T) {
	require.Equal(t, "build/generated/logs/seid-2-restart.log", seidRestartLogPath("sei-node-2"))
	require.Equal(t, "build/generated/logs/seid-0-restart.log", seidRestartLogPath("sei-node-0"))
}

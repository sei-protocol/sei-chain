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

func TestParseBroadcastTxHash(t *testing.T) {
	hash, err := parseBroadcastTxHash(`{"txhash":"ABCDEF","code":0}`)
	require.NoError(t, err)
	require.Equal(t, "ABCDEF", hash)

	_, err = parseBroadcastTxHash(`{"code":0}`)
	require.ErrorContains(t, err, "no txhash")
}

func TestParseDeliveredTx(t *testing.T) {
	got, err := parseDeliveredTx(`{
  "txhash": "ABCDEF",
  "height": "42",
  "code": 0,
  "gas_used": "12345",
  "raw_log": "[]"
}`)
	require.NoError(t, err)
	require.Equal(t, DeliveredTx{
		Hash:    "ABCDEF",
		Height:  42,
		Code:    0,
		GasUsed: 12345,
		RawLog:  "[]",
	}, got)
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

func TestCrossVersionNodes(t *testing.T) {
	require.Equal(t, []string{"sei-node-0", "sei-node-1", "sei-node-2", "sei-node-3"},
		(&CrossVersion{}).Nodes())
}

func TestParseBlockIdentity(t *testing.T) {
	parsed, err := parseBlockIdentity([]byte(`{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "block_id": {
      "hash": "0x01020304"
    },
    "block": {
      "header": {
        "height": "42",
        "app_hash": "0x0a0b0c0d"
      }
    }
  }
}`))
	require.NoError(t, err)
	require.Equal(t, int64(42), parsed.height)
	require.Equal(t, []byte{0x0a, 0x0b, 0x0c, 0x0d}, parsed.appHash)
	require.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, parsed.blockHash)

	_, err = parseBlockIdentity([]byte(`{"result":{"block_id":{"hash":"01"},"block":{"header":{"height":"1","app_hash":""}}}}`))
	require.ErrorContains(t, err, "no app_hash")

	_, err = parseBlockIdentity([]byte(`{"result":{"block_id":{"hash":""},"block":{"header":{"height":"1","app_hash":"0a"}}}}`))
	require.ErrorContains(t, err, "no block_hash")
}

func TestValidatorBlockAgreementError(t *testing.T) {
	agreeing := []blockView{
		{node: "sei-node-0", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
		{node: "sei-node-1", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
		{node: "sei-node-2", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
		{node: "sei-node-3", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
	}
	require.NoError(t, validatorBlockAgreementError(67, agreeing))
	require.ErrorContains(t, validatorBlockAgreementError(67, agreeing[:1]),
		"need at least two validators to compare at height 67")

	splitState := []blockView{
		{node: "sei-node-0", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
		{node: "sei-node-1", appHash: []byte{0xcc}, blockHash: []byte{0xbb}},
		{node: "sei-node-2", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
		{node: "sei-node-3", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
	}
	err := validatorBlockAgreementError(67, splitState)
	require.Error(t, err)
	require.ErrorContains(t, err, "validators disagreed at height 67")
	require.ErrorContains(t, err, "sei-node-0 app_hash=aa block_hash=bb")
	require.ErrorContains(t, err, "sei-node-1 app_hash=cc block_hash=bb")

	splitChain := []blockView{
		{node: "sei-node-0", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
		{node: "sei-node-1", appHash: []byte{0xaa}, blockHash: []byte{0xdd}},
		{node: "sei-node-2", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
		{node: "sei-node-3", appHash: []byte{0xaa}, blockHash: []byte{0xbb}},
	}
	err = validatorBlockAgreementError(68, splitChain)
	require.Error(t, err)
	require.ErrorContains(t, err, "validators disagreed at height 68")
	require.ErrorContains(t, err, "sei-node-0 app_hash=aa block_hash=bb")
	require.ErrorContains(t, err, "sei-node-1 app_hash=aa block_hash=dd")
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

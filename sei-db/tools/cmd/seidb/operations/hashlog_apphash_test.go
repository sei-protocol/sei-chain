package operations

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/stretchr/testify/require"
)

var appHashTestColumns = []string{appHashColumn, "blockHash", "resultHash"}

// appHashTestBlock is one block's hashes; nil fields are not reported, so the block is not written.
type appHashTestBlock struct {
	app, block, result []byte
}

func writeAppHashTestArchive(t *testing.T, blocks map[uint64]appHashTestBlock) string {
	t.Helper()
	dir := t.TempDir()
	logger, err := hashlog.NewHashLogger(&hashlog.HashLoggerConfig{
		Path:                    dir,
		Version:                 "v1.0.0",
		HashTypes:               appHashTestColumns,
		DisableChangesetHashing: true,
		HashBufferSize:          64,
		WriteBufferSize:         64,
		ControlBufferSize:       64,
		MaxBufferedBlocks:       1024,
		BlocksToRetain:          1024,
		TargetFileSize:          unit.MB,
		MaxDiskSize:             unit.GB,
	})
	require.NoError(t, err)
	for height := uint64(1); height <= 10; height++ {
		b, ok := blocks[height]
		if !ok {
			continue
		}
		require.NoError(t, logger.ReportHash(height, appHashColumn, b.app))
		require.NoError(t, logger.ReportHash(height, "blockHash", b.block))
		require.NoError(t, logger.ReportHash(height, "resultHash", b.result))
	}
	require.NoError(t, logger.Close())
	return dir
}

// appHashTestChains returns production and reserve blocks 1..5 with the same history, where the app hash agrees
// at block 1 and differs from block 2 on.
func appHashTestChains() (prod, reserve map[uint64]appHashTestBlock) {
	prod = map[uint64]appHashTestBlock{}
	reserve = map[uint64]appHashTestBlock{}
	for h := uint64(1); h <= 5; h++ {
		shared := appHashTestBlock{app: []byte{0xc0, byte(h)}, block: []byte{0xb0, byte(h)}, result: []byte{0xe0, byte(h)}}
		prod[h] = shared
		if h > 1 {
			shared.app = []byte{0xa0, byte(h)}
		}
		reserve[h] = shared
	}
	return prod, reserve
}

func TestBuildAppHashOverrides(t *testing.T) {
	prod, reserve := appHashTestChains()
	rows, err := buildAppHashOverrides(writeAppHashTestArchive(t, prod), writeAppHashTestArchive(t, reserve),
		1, 5, []string{"blockHash", "resultHash"})
	require.NoError(t, err)
	require.Equal(t, []appHashOverrideRowJSON{
		{Height: 2, Recorded: "C002", Replacement: "A002"},
		{Height: 3, Recorded: "C003", Replacement: "A003"},
		{Height: 4, Recorded: "C004", Replacement: "A004"},
		{Height: 5, Recorded: "C005", Replacement: "A005"},
	}, rows)
}

func TestBuildAppHashOverridesRejects(t *testing.T) {
	match := []string{"blockHash", "resultHash"}
	testcases := map[string]struct {
		mutate    func(prod, reserve map[uint64]appHashTestBlock)
		low, high uint64
		wantErr   string
	}{
		"history differs": {func(_, reserve map[uint64]appHashTestBlock) {
			b := reserve[3]
			b.result = []byte{0xff}
			reserve[3] = b
		}, 1, 5, "block 3: resultHash differs"},
		"block missing from reserve": {func(_, reserve map[uint64]appHashTestBlock) {
			delete(reserve, 4)
		}, 1, 5, "block 4 in the reserve archive: no record"},
		"range past both archives": {func(_, _ map[uint64]appHashTestBlock) {}, 1, 6,
			"block 6 in the production archive: no record"},
		"app hashes agree": {func(_, _ map[uint64]appHashTestBlock) {}, 1, 1, "no override is needed"},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			prod, reserve := appHashTestChains()
			tc.mutate(prod, reserve)
			_, err := buildAppHashOverrides(writeAppHashTestArchive(t, prod), writeAppHashTestArchive(t, reserve),
				tc.low, tc.high, match)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestHashLogAppHashOverridesCmdRejectsEmptyChainID(t *testing.T) {
	cmd := HashLogCmd()
	cmd.SetArgs([]string{"apphash-overrides", "missing-a", "missing-b", "--chain-id", "", "--low", "1", "--high", "2"})
	require.PanicsWithValue(t, "--chain-id must not be empty", func() { _ = cmd.Execute() })
}

func TestHashLogAppHashOverridesCmdWritesFile(t *testing.T) {
	prod, reserve := appHashTestChains()
	output := filepath.Join(t.TempDir(), "overrides.json")
	cmd := HashLogCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"apphash-overrides", writeAppHashTestArchive(t, prod), writeAppHashTestArchive(t, reserve),
		"--chain-id", "test-chain", "--low", "2", "--high", "3", "--match", "blockHash,resultHash",
		"--source", "unit test", "-o", output})
	require.NoError(t, cmd.Execute())
	require.Contains(t, stderr.String(), "2 override(s) for test-chain over blocks [2, 3]")

	data, err := os.ReadFile(output) //nolint:gosec // test temp file
	require.NoError(t, err)
	var file appHashOverrideFileJSON
	require.NoError(t, json.Unmarshal(data, &file))
	require.Equal(t, appHashOverrideFileJSON{
		ChainID: "test-chain",
		Source:  "unit test",
		Overrides: []appHashOverrideRowJSON{
			{Height: 2, Recorded: "C002", Replacement: "A002"},
			{Height: 3, Recorded: "C003", Replacement: "A003"},
		},
	}, file)
}

func TestHashLogRewindCmdRejectsEmptyChainID(t *testing.T) {
	cmd := HashLogCmd()
	cmd.SetArgs([]string{"rewind", "missing", "--chain-id", "", "--safe-height", "1", "--high", "2"})
	require.PanicsWithValue(t, "--chain-id must not be empty", func() { _ = cmd.Execute() })
}

func TestBuildDiscardedBlocks(t *testing.T) {
	prod, _ := appHashTestChains()
	archive := writeAppHashTestArchive(t, prod)

	discarded, err := buildDiscardedBlocks(archive, 2, 5)
	require.NoError(t, err)
	require.Equal(t, []discardedBlockJSON{
		{Height: 3, Hash: "B003"},
		{Height: 4, Hash: "B004"},
		{Height: 5, Hash: "B005"},
	}, discarded)

	_, err = buildDiscardedBlocks(archive, 2, 4)
	require.ErrorContains(t, err, "holds block 5 above --high 4")
	_, err = buildDiscardedBlocks(archive, 2, 6)
	require.ErrorContains(t, err, "block 6: no record")
	_, err = buildDiscardedBlocks(archive, 4, 4)
	require.Error(t, err)
}

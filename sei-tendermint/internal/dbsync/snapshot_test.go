package dbsync

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/config"
	dstypes "github.com/tendermint/tendermint/proto/tendermint/dbsync"
)

func TestSnapshot(t *testing.T) {
	baseConfig := config.BaseConfig{
		RootDir: t.TempDir(),
		DBPath:  "data",
	}
	appDBDir := path.Join(baseConfig.DBDir(), ApplicationDBSubdirectory)
	os.MkdirAll(appDBDir, os.ModePerm)
	wasmDir := path.Join(baseConfig.RootDir, WasmDirectory)
	os.MkdirAll(wasmDir, os.ModePerm)
	dbsyncConfig := config.DBSyncConfig{
		SnapshotDirectory:   t.TempDir(),
		SnapshotWorkerCount: 1,
	}
	os.MkdirAll(dbsyncConfig.SnapshotDirectory, os.ModePerm)

	dataFilename1, dataFilename2 := "d1", "d2"
	wasmFilename := "w"
	f1, _ := os.Create(path.Join(appDBDir, dataFilename1))
	defer f1.Close()
	f1.WriteString("abc")
	f2, _ := os.Create(path.Join(appDBDir, dataFilename2))
	defer f2.Close()
	f2.WriteString("def")
	w, _ := os.Create(path.Join(wasmDir, wasmFilename))
	defer w.Close()
	w.WriteString("ghi")

	height := uint64(1000)
	err := Snapshot(height, dbsyncConfig, baseConfig)
	require.Nil(t, err)

	// assert snapshot_1000 directory exists
	subdir := path.Join(dbsyncConfig.SnapshotDirectory, fmt.Sprintf("snapshot_%d", height))
	_, err = os.Stat(subdir)
	require.Nil(t, err)

	// assert 3 files + METADATA exist in snapshot_1000
	fds, err := ioutil.ReadDir(subdir)
	require.Nil(t, err)
	expected := map[string]string{"d1": "abc", "d2": "def", "w_wasm": "ghi"}
	checksum1, checksum2, checksum3 := md5.Sum([]byte("abc")), md5.Sum([]byte("def")), md5.Sum([]byte("ghi"))
	expectedMetadata := dstypes.MetadataResponse{
		Height:      height,
		Filenames:   []string{"d1", "d2", "w_wasm"},
		Md5Checksum: [][]byte{checksum1[:], checksum2[:], checksum3[:]},
	}
	serialized, _ := expectedMetadata.Marshal()
	expected["METADATA"] = string(serialized)
	for _, fd := range fds {
		require.Contains(t, expected, fd.Name())
		fp := path.Join(subdir, fd.Name())
		data, err := ioutil.ReadFile(fp)
		require.Nil(t, err)
		require.Equal(t, expected[fd.Name()], string(data))
		delete(expected, fd.Name())
	}

	// assert LATEST_HEIGHT is updated
	hbz, err := ioutil.ReadFile(path.Join(dbsyncConfig.SnapshotDirectory, MetadataHeightFilename))
	require.Nil(t, err)
	writtenHeight, err := strconv.ParseUint(string(hbz), 10, 64)
	require.Nil(t, err)
	require.Equal(t, height, writtenHeight)
}

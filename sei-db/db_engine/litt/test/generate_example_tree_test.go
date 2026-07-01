package test

import (
	"fmt"
	"log"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// TestGenerateExampleTree will generate the example file tree displayed in the readme.
func TestGenerateExampleTree(t *testing.T) {

	t.Skip("this should only be run manually")

	rand := util.NewTestRandom()
	testDir := t.TempDir()

	rootDirectories := []string{path.Join(testDir, "root0"), path.Join(testDir, "root1"), path.Join(testDir, "root2")}

	config, err := litt.DefaultConfig(rootDirectories...)
	require.NoError(t, err)

	config.TargetSegmentFileSize = 100 // use a small value to intentionally create several segments
	config.SnapshotDirectory = path.Join(testDir, "rolling_snapshot")

	tableConfig := litt.DefaultTableConfig("")
	tableConfig.ShardingFactor = 4

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableConfig.Name = "tableA"
	tableA, err := db.BuildTable(tableConfig)
	require.NoError(t, err)
	tableConfig.Name = "tableB"
	tableB, err := db.BuildTable(tableConfig)
	require.NoError(t, err)
	tableConfig.Name = "tableC"
	tableC, err := db.BuildTable(tableConfig)
	require.NoError(t, err)

	// Write enough data to tableA to create 3 segments
	err = tableA.Put([]byte("key1"), rand.Bytes(100))
	require.NoError(t, err)
	err = tableA.Put([]byte("key2"), rand.Bytes(100))
	require.NoError(t, err)
	err = tableA.Put([]byte("key3"), rand.Bytes(100))
	require.NoError(t, err)

	// Write enough data to tableB to create 2 segments
	err = tableB.Put([]byte("key1"), rand.Bytes(100))
	require.NoError(t, err)
	err = tableB.Put([]byte("key2"), rand.Bytes(100))
	require.NoError(t, err)

	// Write enough data to tableC to create 1 segment
	err = tableC.Put([]byte("key1"), rand.Bytes(50))
	require.NoError(t, err)

	err = tableA.Flush()
	require.NoError(t, err)
	err = tableB.Flush()
	require.NoError(t, err)
	err = tableC.Flush()
	require.NoError(t, err)

	// Simulate a lower bound files. This normally only gets generated when there is GC done externally.
	for _, tableName := range []string{"tableA", "tableB", "tableC"} {
		lowerBoundFile, err := disktable.LoadBoundaryFile(
			disktable.LowerBound,
			path.Join(testDir, "rolling_snapshot", tableName))
		require.NoError(t, err)
		err = lowerBoundFile.Update(0)
		require.NoError(t, err)
	}

	// Simulate a gc-watermark file. This normally only gets created once garbage collection has run. It lives in
	// the table directory in root0 (the same root that holds the keymap), so it survives a keymap rebuild.
	for _, tableName := range []string{"tableA", "tableB", "tableC"} {
		gcWatermarkFile, err := disktable.LoadGCWatermarkFile(path.Join(rootDirectories[0], tableName))
		require.NoError(t, err)
		err = gcWatermarkFile.Update(0)
		require.NoError(t, err)
	}

	// Run the tree command on testDir
	output, err := exec.Command("tree", testDir).CombinedOutput()
	if err != nil {
		log.Fatalf("command failed: %v", err)
	}
	// Convert the output (a byte slice) into a string
	resultString := string(output)

	// replace the root name with "root".
	resultString = strings.ReplaceAll(resultString, testDir, "root")

	fmt.Println(resultString)

	err = db.Close()
	require.NoError(t, err)
}

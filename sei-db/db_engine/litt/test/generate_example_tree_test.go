package test

import (
	"fmt"
	"log"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/disktable"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

// TestGenerateExampleTree will generate the example file tree displayed in the readme.
func TestGenerateExampleTree(t *testing.T) {

	t.Skip("this should only be run manually")

	rand := random.NewTestRandom()
	testDir := t.TempDir()

	rootDirectories := []string{path.Join(testDir, "root0"), path.Join(testDir, "root1"), path.Join(testDir, "root2")}

	config, err := litt.DefaultConfig(rootDirectories...)
	require.NoError(t, err)

	config.ShardingFactor = 4
	config.TargetSegmentFileSize = 100 // use a small value to intentionally create several segments
	config.SnapshotDirectory = path.Join(testDir, "rolling_snapshot")

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableA, err := db.GetTable("tableA")
	require.NoError(t, err)
	tableB, err := db.GetTable("tableB")
	require.NoError(t, err)
	tableC, err := db.GetTable("tableC")
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

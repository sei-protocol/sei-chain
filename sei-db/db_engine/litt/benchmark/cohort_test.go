package benchmark

import (
	"testing"

	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func TestCohortSerialization(t *testing.T) {
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	cohortIndex := rand.Uint64()
	lowIndex := rand.Uint64Range(1, 1000)
	highIndex := rand.Uint64Range(1000, 2000)
	valueSize := rand.Uint64()
	cohort, err := NewCohort(
		testDirectory,
		cohortIndex,
		lowIndex,
		highIndex,
		valueSize,
		false)
	require.NoError(t, err)

	require.Equal(t, cohortIndex, cohort.CohortIndex())
	require.Equal(t, lowIndex, cohort.LowKeyIndex())
	require.Equal(t, highIndex, cohort.HighKeyIndex())
	require.Equal(t, valueSize, cohort.ValueSize())
	require.Equal(t, false, cohort.IsComplete())

	// Check if the cohort file exists
	filePath := cohort.Path()
	exists, err := util.Exists(filePath)
	require.NoError(t, err)
	require.True(t, exists)

	// Initialize a copy cohort from the file
	loadedCohort, err := LoadCohort(cohort.Path())
	require.NoError(t, err)
	require.Equal(t, cohortIndex, loadedCohort.CohortIndex())
	require.Equal(t, lowIndex, loadedCohort.LowKeyIndex())
	require.Equal(t, highIndex, loadedCohort.HighKeyIndex())
	require.Equal(t, valueSize, cohort.ValueSize())
	require.Equal(t, false, loadedCohort.IsComplete())

	// Mark the cohort as written
	loadedCohort.allValuesWritten = true
	require.NoError(t, err)
	require.True(t, loadedCohort.IsComplete())
	err = loadedCohort.Write()
	require.NoError(t, err)

	// Load the cohort again.
	loadedCohort, err = LoadCohort(cohort.Path())
	require.NoError(t, err)
	require.Equal(t, cohortIndex, loadedCohort.CohortIndex())
	require.Equal(t, lowIndex, loadedCohort.LowKeyIndex())
	require.Equal(t, highIndex, loadedCohort.HighKeyIndex())
	require.Equal(t, valueSize, cohort.ValueSize())
	require.Equal(t, true, loadedCohort.IsComplete())

	err = loadedCohort.Delete()
	require.NoError(t, err)

	// The file should no longer exist.
	exists, err = util.Exists(filePath)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestStandardCohortLifecycle(t *testing.T) {
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	cohortIndex := rand.Uint64()
	lowIndex := rand.Uint64Range(1, 1000)
	highIndex := rand.Uint64Range(1000, 2000)
	valueSize := rand.Uint64()
	cohort, err := NewCohort(
		testDirectory,
		cohortIndex,
		lowIndex,
		highIndex,
		valueSize,
		false)
	require.NoError(t, err)

	require.Equal(t, cohortIndex, cohort.CohortIndex())
	require.Equal(t, lowIndex, cohort.LowKeyIndex())
	require.Equal(t, highIndex, cohort.HighKeyIndex())
	require.Equal(t, valueSize, cohort.ValueSize())
	require.Equal(t, false, cohort.IsComplete())

	// Extract all keys from the cohort.
	for i := lowIndex; i <= highIndex; i++ {
		key, err := cohort.GetKeyIndexForWriting()
		require.NoError(t, err)
		require.Equal(t, i, key)

		shouldBeExhausted := i == highIndex
		require.Equal(t, shouldBeExhausted, cohort.IsExhausted())

		if i < highIndex {
			// Attempting to mark as complete now should fail.
			err = cohort.MarkComplete()
			require.Error(t, err)
		}
		require.Equal(t, false, cohort.IsComplete())

		// Attempting to get a key for reading should fail.
		_, err = cohort.GetKeyIndexForReading(rand.Rand)
		require.Error(t, err)
	}

	// Attempting to allocate another key for writing should fail.
	_, err = cohort.GetKeyIndexForWriting()
	require.Error(t, err)

	// We can now mark the cohort as complete.
	err = cohort.MarkComplete()
	require.NoError(t, err)
	require.Equal(t, true, cohort.IsComplete())

	// We can now get keys for reading.
	for i := 0; i < 100; i++ {
		key, err := cohort.GetKeyIndexForReading(rand.Rand)
		require.NoError(t, err)
		require.GreaterOrEqual(t, key, lowIndex)
		require.LessOrEqual(t, key, highIndex)
	}

	// Marking complete again should fail.
	err = cohort.MarkComplete()
	require.Error(t, err)
}

func TestIncompleteCohortAllKeysExtractedLifecycle(t *testing.T) {
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	cohortIndex := rand.Uint64()
	lowIndex := rand.Uint64Range(1, 1000)
	highIndex := rand.Uint64Range(1000, 2000)
	valueSize := rand.Uint64()
	cohort, err := NewCohort(
		testDirectory,
		cohortIndex,
		lowIndex,
		highIndex,
		valueSize,
		false)
	require.NoError(t, err)

	require.Equal(t, cohortIndex, cohort.CohortIndex())
	require.Equal(t, lowIndex, cohort.LowKeyIndex())
	require.Equal(t, highIndex, cohort.HighKeyIndex())
	require.Equal(t, valueSize, cohort.ValueSize())
	require.Equal(t, cohort.IsComplete(), false)

	// Extract all keys from the cohort.
	for i := lowIndex; i <= highIndex; i++ {
		key, err := cohort.GetKeyIndexForWriting()
		require.NoError(t, err)
		require.Equal(t, i, key)

		shouldBeExhausted := i == highIndex
		require.Equal(t, shouldBeExhausted, cohort.IsExhausted())

		if i < highIndex {
			// Attempting to mark as complete now should fail.
			err = cohort.MarkComplete()
			require.Error(t, err)
		}
		require.Equal(t, false, cohort.IsComplete())

		// Attempting to get a key for reading should fail.
		_, err = cohort.GetKeyIndexForReading(rand.Rand)
		require.Error(t, err)
	}

	// Simulate a benchmark restart by reloading the cohort from disk.
	loadedCohort, err := LoadCohort(cohort.Path())
	require.NoError(t, err)

	require.Equal(t, loadedCohort.CohortIndex(), cohortIndex)
	require.False(t, loadedCohort.IsComplete())

	// Attempting to allocate another key for writing should fail.
	_, err = loadedCohort.GetKeyIndexForWriting()
	require.Error(t, err)

	// Attempting to get a key for reading should fail.
	_, err = loadedCohort.GetKeyIndexForReading(rand.Rand)
	require.Error(t, err)

	// We shouldn't be able to mark the cohort as complete.
	err = loadedCohort.MarkComplete()
	require.Error(t, err)
}

func TestIncompleteCohortSomeKeysExtractedLifecycle(t *testing.T) {
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	cohortIndex := rand.Uint64()
	lowIndex := rand.Uint64Range(1, 1000)
	highIndex := rand.Uint64Range(1000, 2000)
	valueSize := rand.Uint64()
	cohort, err := NewCohort(
		testDirectory,
		cohortIndex,
		lowIndex,
		highIndex,
		valueSize,
		false)
	require.NoError(t, err)

	require.Equal(t, cohortIndex, cohort.CohortIndex())
	require.Equal(t, lowIndex, cohort.LowKeyIndex())
	require.Equal(t, highIndex, cohort.HighKeyIndex())
	require.Equal(t, valueSize, cohort.ValueSize())
	require.Equal(t, false, cohort.IsComplete())

	// Extract all keys from the cohort.
	for i := lowIndex; i <= (lowIndex+highIndex)/2; i++ {
		key, err := cohort.GetKeyIndexForWriting()
		require.NoError(t, err)
		require.Equal(t, i, key)

		require.Equal(t, false, cohort.IsExhausted())

		// Attempting to mark as complete now should fail.
		err = cohort.MarkComplete()
		require.Error(t, err)
		require.Equal(t, false, cohort.IsComplete())

		// Attempting to get a key for reading should fail.
		_, err = cohort.GetKeyIndexForReading(rand.Rand)
		require.Error(t, err)
	}

	// Simulate a benchmark restart by reloading the cohort from disk.
	loadedCohort, err := LoadCohort(cohort.Path())
	require.NoError(t, err)

	require.Equal(t, loadedCohort.CohortIndex(), cohortIndex)
	require.False(t, loadedCohort.IsComplete())

	// Attempting to allocate another key for writing should fail.
	_, err = loadedCohort.GetKeyIndexForWriting()
	require.Error(t, err)

	// Attempting to get a key for reading should fail.
	_, err = loadedCohort.GetKeyIndexForReading(rand.Rand)
	require.Error(t, err)

	// We shouldn't be able to mark the cohort as complete.
	err = loadedCohort.MarkComplete()
	require.Error(t, err)
}

func TestNextCohort(t *testing.T) {
	rand := random.NewTestRandom()
	testDirectory := t.TempDir()

	cohortIndex := rand.Uint64()
	lowIndex := rand.Uint64Range(1, 1000)
	highIndex := rand.Uint64Range(1000, 2000)
	valueSize := rand.Uint64()
	cohort, err := NewCohort(
		testDirectory,
		cohortIndex,
		lowIndex,
		highIndex,
		valueSize,
		false)
	require.NoError(t, err)

	require.Equal(t, cohortIndex, cohort.CohortIndex())
	require.Equal(t, lowIndex, cohort.LowKeyIndex())
	require.Equal(t, highIndex, cohort.HighKeyIndex())
	require.Equal(t, valueSize, cohort.ValueSize())
	require.Equal(t, false, cohort.IsComplete())

	// Check if the cohort file exists
	filePath := cohort.Path()
	exists, err := util.Exists(filePath)
	require.NoError(t, err)
	require.True(t, exists)

	newKeyCount := rand.Uint64Range(1, 1000)
	newValueSize := rand.Uint64Range(1, 1000)
	nextCohort, err := cohort.NextCohort(newKeyCount, newValueSize)

	require.NoError(t, err)

	require.Equal(t, cohortIndex+1, nextCohort.CohortIndex())
	require.Equal(t, highIndex+1, nextCohort.LowKeyIndex())
	require.Equal(t, highIndex+newKeyCount, nextCohort.HighKeyIndex())
	require.Equal(t, newValueSize, nextCohort.ValueSize())
	require.Equal(t, false, nextCohort.IsComplete())

	// Check if the next cohort file exists
	nextFilePath := nextCohort.Path()
	exists, err = util.Exists(nextFilePath)
	require.NoError(t, err)
	require.True(t, exists)
}

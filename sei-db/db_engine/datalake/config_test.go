package datalake

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDataLakeConfigValidate(t *testing.T) {
	require.Error(t, DefaultDataLakeConfig().Validate(), "no bucket URL")
	require.NoError(t, DefaultTestDataLakeConfig(t.TempDir()).Validate())

	invalid := []func(*DataLakeConfig){
		func(c *DataLakeConfig) { c.BucketURL = "" },
		func(c *DataLakeConfig) { c.MetadataWriteInterval = 0 },
		func(c *DataLakeConfig) { c.WriterCount = 0 },
		func(c *DataLakeConfig) { c.ReaderCount = 0 },
		func(c *DataLakeConfig) { c.WorkBufferSize = 0 },
		func(c *DataLakeConfig) { c.RequestBufferSize = 0 },
		func(c *DataLakeConfig) { c.DeletesPerCycle = 1 },
		func(c *DataLakeConfig) { c.RetryBackoff = -time.Second },
		func(c *DataLakeConfig) { c.MaxRetries = -1 },
		func(c *DataLakeConfig) { c.PrefetchDepth = 0 },
	}
	for i, mutate := range invalid {
		config := DefaultTestDataLakeConfig(t.TempDir())
		mutate(&config)
		require.Error(t, config.Validate(), "case %d", i)
	}
}

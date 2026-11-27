package config

import "github.com/sei-protocol/sei-chain/sei-db/db_engine/cdkdatalake"

// DefaultDataLakeConfig returns the configuration every data lake of a Giga node opens with, in the bucket
// bucketURL names.
func DefaultDataLakeConfig(bucketURL string) cdkdatalake.DataLakeConfig {
	config := cdkdatalake.DefaultDataLakeConfig()
	config.BucketURL = bucketURL
	// Each queued put holds a whole record, so the queues are sized for memory rather than throughput.
	config.RequestBufferSize = 64
	config.WorkBufferSize = 64
	return config
}

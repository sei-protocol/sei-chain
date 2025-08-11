package dbbackend

type DBBackend interface {
	BenchmarkDBWrite(inputKVDir string, numVersions int, outputDBPath string, concurrency int, batchSize int)
	BenchmarkDBRead(inputKVDir string, numVersions int, outputDBPath string, concurrency int, maxOps int64)
	BenchmarkDBForwardIteration(inputKVDir string, numVersions int, outputDBPath string, concurrency int, maxOps int64, iterationSteps int)
	BenchmarkDBReverseIteration(inputKVDir string, numVersions int, outputDBPath string, concurrency int, maxOps int64, iterationSteps int)
}

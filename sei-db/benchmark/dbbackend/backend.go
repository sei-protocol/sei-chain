package dbbackend

type DBBackend interface {
	BenchmarkDBWrite(inputKVDir string, outputDBPath string, concurrency int, maxRetries int)
	BenchmarkDBRead(inputKVDir string, outputDBPath string, concurrency int)
}

type RocksDBBackend struct{}

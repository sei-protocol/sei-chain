package benchmark

const RocksDBBackendName = "rocksdb"
const PebbleDBBackendName = "pebbledb"

var (

	// TODO: Will include rocksdb, pebbledb and sqlite in future PR's
	ValidDBBackends = map[string]bool{
		RocksDBBackendName:  true,
		PebbleDBBackendName: true,
	}
)

package lthash

// The vocabulary shared by everything that hashes a block: the engine's inputs, its per-module
// intermediates, and the state it produces.

// KeyMutation holds a KV change for LtHash computation.
type KeyMutation struct {
	Key       []byte
	Value     []byte
	LastValue []byte // Previous value (nil for new keys)
	Delete    bool   // If true, only remove last value
}

// DatabaseMutations is everything one database changed in a block.
type DatabaseMutations struct {
	DBName    string
	Mutations []KeyMutation
}

// ModuleParser extracts the owning module name from a physical key. Injected by
// the caller so that hashing stays decoupled from the key-encoding package.
type ModuleParser func(physicalKey []byte) (module string, err error)

// ModuleKey identifies a single (database, module) accumulator.
type ModuleKey struct {
	DBName string
	Module string
}

// ModuleHashInfo is the per-(database, module) change computed for one block/batch:
// the homomorphic hash delta plus the net key-count and byte deltas implied by
// the same MixIn/MixOut transitions.
type ModuleHashInfo struct {
	Hash     *LtHash
	KeyCount int64
	Bytes    int64
}

// BlockHash is the complete lattice hash state as of one block: what hashing a block produces, what the
// engine publishes, and what it is seeded from. A value may be held for as long as its reader wants,
// and later blocks do not disturb it.
type BlockHash struct {
	// BlockNumber is the height this state describes.
	BlockNumber int64

	// PerDB is each data database's lattice hash root, with an entry for every database the engine was
	// configured with, so a caller can swap the map in wholesale.
	PerDB map[string]*LtHash

	// PerModule is each database's per-module lattice hashes, keyed by database name then module. A
	// database's root in PerDB is the homomorphic sum of its entries here.
	PerModule map[string]map[string]*LtHash

	// PerModuleStats is each database's per-module key-count and byte totals, combined alongside the
	// hashes and by the same membership rule. Consensus-irrelevant, but persisted and validated on load.
	PerModuleStats map[string]map[string]ModuleStats

	// Global is the store-wide root, the homomorphic sum of the per-DB roots. This is the value that
	// reaches consensus.
	Global *LtHash

	// Error is the failure that stopped this block from being hashed, and is set only on a value
	// delivered by the engine's stream. Nil everywhere else, including on a seed.
	Error error
}

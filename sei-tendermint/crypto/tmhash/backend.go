package tmhash

import (
	"os"
	"slices"
)

// BackendEnv is the environment variable that pins the batch hashing backend
// by name. An unknown or empty value leaves the selection automatic.
const BackendEnv = "SEI_TMHASH_BACKEND"

// backend is one implementation of batched SHA-256. Every backend produces
// bit-identical digests; they differ only in how fast they get there.
type backend struct {
	name string
	// lanes is the number of equal-length messages the backend hashes at
	// once; 1 means one message at a time.
	lanes int
	// sumBatch writes SHA-256(prefix || msgs[i]) to out[i] for every i.
	sumBatch func(prefix []byte, msgs [][]byte, out [][Size]byte)
}

var active = selectBackend(os.Getenv(BackendEnv))

// ActiveBackend returns the name of the batch hashing backend in use.
func ActiveBackend() string {
	return active.name
}

// BatchLanes returns how many messages the active backend hashes in parallel.
// Callers batching work should hand over multiples of this many messages.
func BatchLanes() int {
	return active.lanes
}

// SumBatch writes SHA-256(prefix || msgs[i]) to out[i] for every i.
// out must be at least as long as msgs.
func SumBatch(prefix []byte, msgs [][]byte, out [][Size]byte) {
	active.sumBatch(prefix, msgs, out)
}

// availableBackends returns every backend this binary can run on this CPU,
// keyed by name.
func availableBackends() map[string]backend {
	m := map[string]backend{defaultBackend.name: defaultBackend}
	if b, ok := simdBackend(); ok {
		m[b.name] = b
	}
	return m
}

// availableBackendNames returns the names from availableBackends, sorted.
func availableBackendNames() []string {
	m := availableBackends()
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// selectBackend picks the backend named by pin, or the fastest available one
// when pin is empty or unknown.
func selectBackend(pin string) backend {
	if b, ok := availableBackends()[pin]; ok {
		return b
	}
	if b, ok := simdBackend(); ok {
		return b
	}
	return defaultBackend
}

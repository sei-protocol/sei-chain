package lthash

import (
	"os"
	"sort"
)

// BackendEnv is the environment variable that pins the hashing backend by
// name. An unknown or empty value leaves the selection automatic.
const BackendEnv = "SEI_LTHASH_BACKEND"

// backend is one implementation of the LtHash primitives. Every backend must
// produce bit-identical results; they differ only in how fast they get there.
type backend struct {
	name string
	// expand fills dst with the 2048-byte Blake3 XOF of data, one
	// little-endian uint16 per limb. data is never empty.
	expand func(data []byte, dst *LtHash)
	// add and sub are element-wise mod 2^16 on the limb vectors.
	add func(dst, src *LtHash)
	sub func(dst, src *LtHash)
	// newAccumulator returns an accumulator over this backend's expansion.
	newAccumulator func() accumulator
}

// accumulator sums a sequence of expansions into one LtHash. A backend may
// accumulate in whatever limb order its expansion produces; finish is what puts
// the sum in limb order.
type accumulator interface {
	// fold mixes the expansion of data into the running sum, subtracting it
	// instead of adding it when subtract is true. data is never empty.
	fold(data []byte, subtract bool)
	// finish writes the running sum to dst, replacing its limbs.
	finish(dst *LtHash)
}

var active = selectBackend(os.Getenv(BackendEnv))

// ActiveBackend returns the name of the hashing backend in use.
func ActiveBackend() string {
	return active.name
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
	sort.Strings(names)
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

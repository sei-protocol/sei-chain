//go:build jemalloc && cgo

// Package jemalloc links jemalloc into seid so every C-heap allocation made
// through cgo (Pebble's block cache and memtables, DataDog/zstd buffers,
// libwasmvm) is served by jemalloc instead of the platform malloc. Build with
// `-tags jemalloc` (or `make build BUILD_TAGS=jemalloc`) against a jemalloc
// whose headers and library are on the cgo search path; see `make
// build-jemalloc-lib`. Without the tag the package is a no-op and the platform
// allocator is used, so the default build is unchanged.
package jemalloc

/*
#cgo LDFLAGS: -ljemalloc
#include <stddef.h>
#include <stdint.h>
#include <jemalloc/jemalloc.h>

static const char *sei_jemalloc_version(void) {
	const char *v = NULL;
	size_t sz = sizeof(v);
	if (mallctl("version", &v, &sz, NULL, 0) != 0) {
		return NULL;
	}
	return v;
}

static int sei_jemalloc_allocated(size_t *out) {
	uint64_t epoch = 1;
	size_t esz = sizeof(epoch);
	// Statistics are cached; bumping the epoch refreshes them.
	if (mallctl("epoch", &epoch, &esz, &epoch, esz) != 0) {
		return -1;
	}
	size_t sz = sizeof(*out);
	return mallctl("stats.allocated", out, &sz, NULL, 0);
}
*/
import "C"

// Enabled reports whether this binary was built with the jemalloc build tag.
const Enabled = true

// Version returns the version string of the linked jemalloc, or "" if the
// runtime did not answer.
func Version() string {
	v := C.sei_jemalloc_version()
	if v == nil {
		return ""
	}
	return C.GoString(v)
}

// Allocated returns the bytes currently allocated by the application through
// jemalloc (the `stats.allocated` mallctl), or 0 if statistics are unavailable.
func Allocated() uint64 {
	var out C.size_t
	if C.sei_jemalloc_allocated(&out) != 0 {
		return 0
	}
	return uint64(out)
}

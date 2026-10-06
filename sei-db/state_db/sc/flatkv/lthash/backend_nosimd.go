//go:build !(goexperiment.simd && amd64)

package lthash

// simdBackend reports that no SIMD backend is compiled into this binary.
func simdBackend() (backend, bool) {
	return backend{}, false
}

//go:build !(goexperiment.simd && amd64)

package tmhash

func simdBackend() (backend, bool) {
	return backend{}, false
}

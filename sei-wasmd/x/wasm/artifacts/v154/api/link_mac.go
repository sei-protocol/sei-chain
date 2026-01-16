//go:build darwin && !static_wasm && !sys_wasmvm

package api

// #cgo LDFLAGS: -Wl,-rpath,${SRCDIR} -L${SRCDIR} -lwasmvm154
import "C"

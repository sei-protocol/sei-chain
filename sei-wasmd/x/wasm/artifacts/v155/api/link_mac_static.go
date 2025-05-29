//go:build darwin && static_wasm && !sys_wasmvm

package api

// #cgo LDFLAGS: -L${SRCDIR} -lwasmvm155static_darwin
import "C"

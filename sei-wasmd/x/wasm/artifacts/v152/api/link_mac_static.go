//go:build darwin && static_wasm && !sys_wasmvm

package api

// #cgo LDFLAGS: -L${SRCDIR} -lwasmvm152static_darwin
import "C"

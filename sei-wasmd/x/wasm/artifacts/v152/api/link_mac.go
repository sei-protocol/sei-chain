//go:build darwin && !static_wasm && !sys_wasmvm

package api

// #cgo LDFLAGS: -Wl,-rpath,@loader_path -L${SRCDIR} -lwasmvm152
import "C"

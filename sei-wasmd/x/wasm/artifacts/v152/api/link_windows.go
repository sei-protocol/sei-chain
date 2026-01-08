//go:build windows && !sys_wasmvm

package api

// #cgo LDFLAGS: -Wl,-rpath,${ORIGIN} -L${SRCDIR} -lwasmvm152
import "C"

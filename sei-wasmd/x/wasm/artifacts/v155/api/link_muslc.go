//go:build linux && muslc && !sys_wasmvm

package api

// #cgo LDFLAGS: -Wl,-rpath,${SRCDIR} -L${SRCDIR} -lwasmvm155_muslc
import "C"

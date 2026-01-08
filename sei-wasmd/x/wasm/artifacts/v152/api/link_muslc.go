//go:build linux && muslc && !sys_wasmvm

package api

// #cgo LDFLAGS: -Wl,-rpath,${ORIGIN} -L${SRCDIR} -lwasmvm152_muslc
import "C"

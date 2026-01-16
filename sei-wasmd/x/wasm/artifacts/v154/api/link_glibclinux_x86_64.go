//go:build linux && !muslc && amd64 && !sys_wasmvm

package api

// #cgo LDFLAGS: -Wl,-rpath,${SRCDIR} -L${SRCDIR} -lwasmvm154.x86_64
import "C"

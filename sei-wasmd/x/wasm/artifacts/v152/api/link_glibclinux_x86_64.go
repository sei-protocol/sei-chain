//go:build linux && !muslc && amd64 && !sys_wasmvm

package api

// #cgo LDFLAGS: -Wl,-rpath,${ORIGIN} -L${SRCDIR} -lwasmvm152.x86_64
import "C"

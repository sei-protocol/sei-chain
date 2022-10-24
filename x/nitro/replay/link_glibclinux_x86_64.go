//go:build !codeanalysis && linux && amd64 && !sys_nitro_replayer
// +build !codeanalysis,linux,amd64,!sys_nitro_replayer

package replay

// #cgo LDFLAGS: -Wl,-rpath,${SRCDIR} -L${SRCDIR} -lnitro_replayer.x86_64
import "C"

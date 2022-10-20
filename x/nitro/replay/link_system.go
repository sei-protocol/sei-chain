//go:build !codeanalysis && sys_replayer
// +build !codeanalysis,sys_replayer

package replay

// #cgo LDFLAGS: -lreplayer
import "C"

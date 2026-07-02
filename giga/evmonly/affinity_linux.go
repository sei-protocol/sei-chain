//go:build linux

package evmonly

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

func pinCurrentWorkerThread(cpu int) (func(), error) {
	runtime.LockOSThread()
	unlock := runtime.UnlockOSThread
	numCPU := runtime.NumCPU()
	if numCPU <= 0 {
		unlock()
		return func() {}, fmt.Errorf("runtime reported no CPUs")
	}
	if cpu < 0 {
		unlock()
		return func() {}, fmt.Errorf("CPU index must be non-negative")
	}
	cpu %= numCPU
	var set unix.CPUSet
	set.Set(cpu)
	if err := unix.SchedSetaffinity(0, &set); err != nil {
		unlock()
		return func() {}, err
	}
	return unlock, nil
}

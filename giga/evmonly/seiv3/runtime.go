package seiv3

import "runtime"

func runtimeCPU() int {
	return runtime.NumCPU()
}

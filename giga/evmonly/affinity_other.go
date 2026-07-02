//go:build !linux

package evmonly

import "fmt"

func pinCurrentWorkerThread(int) (func(), error) {
	return func() {}, fmt.Errorf("worker CPU pinning is only supported on linux")
}

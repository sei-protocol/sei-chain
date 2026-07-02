//go:build !linux

package main

import "fmt"

func pinCurrentWorkerThread(int) (func(), error) {
	return func() {}, fmt.Errorf("worker CPU pinning is only supported on linux")
}

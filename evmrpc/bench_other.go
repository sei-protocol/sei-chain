//go:build !linux

package evmrpc

import "errors"

func benchPin(cpu int) (func(), error) { return nil, errors.New("pinning requires linux") }
func benchSiblingOf(cpu int) int       { return -1 }

type benchCPUT struct{ busy, total uint64 }

func benchCPUTimes() []benchCPUT                  { return nil }
func benchBusyBetween(a, b []benchCPUT) []float64 { return nil }

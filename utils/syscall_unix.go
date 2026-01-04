//go:build linux || darwin
// +build linux darwin

package utils

import "golang.org/x/sys/unix"

func SetNonblock(fd int, nonblocking bool) error {
	return unix.SetNonblock(fd, nonblocking)
}

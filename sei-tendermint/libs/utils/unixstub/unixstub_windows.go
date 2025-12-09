// +build windows

package unixstub

import "errors"

func SetNonblock(fd int, nonblocking bool) error {
    return errors.New("SetNonblock not supported on Windows")
}

func CloseOnExec(fd int) error {
    return errors.New("CloseOnExec not supported on Windows")
}

func Socket(domain, typ, proto int) (int, error) {
    return 0, errors.New("Socket not supported on Windows")
}

func SetsockoptInt(fd, level, opt, value int) error {
    return errors.New("SetsockoptInt not supported on Windows")
}

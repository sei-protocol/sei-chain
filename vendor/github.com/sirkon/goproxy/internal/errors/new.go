package errors

import (
	"errors"
	"fmt"
)

// New a copy of stdlib errors.New
func New(msg string) error {
	return errors.New(msg)
}

// Newf just calls fmt.Errorf
func Newf(format string, a ...interface{}) error {
	return fmt.Errorf(format, a...)
}

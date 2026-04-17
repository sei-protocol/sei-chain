package errors

import (
	"errors"
)

var (
	ErrKeyEmpty            = errors.New("key empty")
	ErrRecordNotFound      = errors.New("record not found")
	ErrStartAfterEnd       = errors.New("start key after end key")
	ErrorExportDone        = errors.New("export is complete")
	ErrNotFound            = errors.New("not found")
	ErrFileLockUnavailable = errors.New("file lock unavailable")
)

// IsNotFound returns true if the error represents a "not found" condition.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsFileLockError returns true if the error is due to a file lock
// that could not be acquired (e.g. held by another process).
func IsFileLockError(err error) bool {
	return errors.Is(err, ErrFileLockUnavailable)
}

// Join returns an error that wraps the given errors.
// Any nil error values are discarded.
// Join returns nil if errs contains no non-nil values.
// Unlike the previous string-concatenation implementation, this delegates
// to stdlib errors.Join so that wrapped sentinels remain detectable via
// errors.Is / errors.As.
func Join(errs ...error) error {
	return errors.Join(errs...)
}

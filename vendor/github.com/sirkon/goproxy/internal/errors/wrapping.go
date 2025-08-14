package errors

import (
	"bytes"
	"fmt"
)

var _ error = &wrappedError{}

type wrappedError struct {
	msgs []string
	err  error
}

func (err *wrappedError) Error() string {
	var buf bytes.Buffer
	for i := len(err.msgs) - 1; i >= 0; i-- {
		buf.WriteString(err.msgs[i])
		buf.WriteString(": ")
	}
	buf.WriteString(err.err.Error())
	return buf.String()
}

// Unwrap unwraps given error returning underlying err
func (err *wrappedError) Unwrap() error {
	return err.err
}

// Wrap wraps an error with given message
func Wrap(err error, msg string) error {
	switch v := err.(type) {
	case *wrappedError:
		v.msgs = append(v.msgs, msg)
		return v
	default:
		return &wrappedError{
			msgs: []string{msg},
			err:  err,
		}
	}
}

// Wrapf wraps an error with given formatted message
func Wrapf(err error, format string, a ...interface{}) error {
	return Wrap(err, fmt.Sprintf(format, a...))
}

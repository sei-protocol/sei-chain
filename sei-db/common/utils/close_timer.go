package utils

import (
	"time"
)

// CloseTimer records how long each of a set of resources took to close, so a shutdown can report one
// breakdown rather than leaving the caller to time each close by hand.
//
// The zero value is ready to use, and it is not safe for concurrent use.
type CloseTimer struct {
	fields []any
}

// Close runs one resource's close under name, recording how long it took, and returns its error
// unchanged so the caller can describe the failure in its own terms.
func (t *CloseTimer) Close(name string, close func() error) error {
	started := time.Now()
	err := close()
	t.fields = append(t.fields, name, time.Since(started).Truncate(time.Millisecond))
	return err
}

// Fields returns each close's name and duration, in the order they ran, as logger key-value pairs.
func (t *CloseTimer) Fields() []any {
	return t.fields
}

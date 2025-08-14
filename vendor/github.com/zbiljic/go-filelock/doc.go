// Package filelock handles file based locking.
//
// While a sync.Mutex helps against concurrency issues within a single process,
// this package is designed to help against concurrency issues between
// cooperating processes or serializing multiple invocations of the same
// process. You can also combine sync.Mutex with Lock in order to serialize
// an action between different goroutines in a single program and also multiple
// invocations of this program.
package filelock

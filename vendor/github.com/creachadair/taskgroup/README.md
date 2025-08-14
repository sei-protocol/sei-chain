# taskgroup

[![GoDoc](https://img.shields.io/static/v1?label=godoc&message=reference&color=blue)](https://pkg.go.dev/github.com/creachadair/taskgroup)

A `*taskgroup.Group` represents a group of goroutines working on related tasks.
New tasks can be added to the group at will, and the caller can wait until all
tasks are complete. Errors are automatically collected and delivered to a
user-provided callback in a single goroutine.  This does not replace the full
generality of Go's built-in features, but it simplifies some of the plumbing
for common concurrent tasks.

Here is a [working example in the Go Playground](https://play.golang.org/p/V2slrnMu2Ec).

## Rationale

Go provides excellent and powerful concurrency primitives, in the form of
[goroutines](http://golang.org/ref/spec#Go_statements),
[channels](http://golang.org/ref/spec#Channel_types),
[select](http://golang.org/ref/spec#Select_statements), and the standard
library's [sync](http://godoc.org/sync) package. In some common situations,
however, managing goroutine lifetimes can become unwieldy using only what is
built in.

For example, consider the case of copying a large directory tree: Walk throught
the source directory recursively, creating the parallel target directory
structure and spinning up a goroutine to copy each of the files
concurrently. This part is simple:

```go
func copyTree(source, target string) error {
	err := filepath.Walk(source, func(path string, fi os.FileInfo, err error) error {
		adjusted := adjustPath(path)
		if fi.IsDir() {
			return os.MkdirAll(adjusted, 0755)
		}
		go copyFile(adjusted, target)
		return nil
	})
	if err != nil {
		// ... clean up the output directory ...
	}
	return err
}
```

Unfortunately, this isn't quite sufficient: How will we detect when all the
file copies are finished? Typically we do this with a `sync.WaitGroup`:

```go
var wg sync.WaitGroup
...
wg.Add(1)
go func() {
    defer wg.Done()
    copyFile(adjusted, target)
}()
...
wg.Wait() // blocks until all the tasks signal done
```

In addition, we need to cope with errors: Copies might fail―the disk might fill
up, or there might be a permissions error, say. For some applications we might
be content to log the error and continue, but often in case of error you'd like
to back out and clean up your mess. To do this, however, we need to capture the
return value from the function inside the goroutine―and that will require us to
plumb in another channel:

```go
errs := make(chan error)
...
go copyFile(adjusted, target, errs)
```

Moreover, we may get more than one error, so we will need another goroutine to
clear it out:

```go
var failures []error
go func() {
    for e := range errs {
        failures = append(failures, e)
    }
}()
...
wg.Wait()
close(errs)
```

But now, how do we know when the error collector is finished so that we can
safely examine the `failures`? We need another channel or `sync.WaitGroup` to
signal for that:

```go
var failures []error
edone := make(chan struct{})
go func() {
    defer close(edone)
    for e := range errs {
        failures = append(failures, e)
	}
}()
...
wg.Wait()   // all the workers are done
close(errs) // signal the error collector to stop
<-edone     // wait for the error collector to be done
```

That's tedious, but works. But now, in this example, if something fails we
really don't want to wait around for all the copies to finish―if one of the
file copies fails, we want to stop what you're doing and clean up.  So now we
need another channel or a context to signal cancellation:

```go
	ctx, cancel := context.WithCancel(context.Background())
	...
	copyFile(ctx, adjusted, target, errs)
```

and then `copyFile` will have to check for that:

```go
func copyFile(ctx context.Context, source, target string, errs chan<- error) {
	select {
	case <-ctx.Done():
		return
	default:
	 	// ... do the copy as normal, or propagate an error
	}
}
```

Finally, the way this is set up right now there is no limit to the number of
copies that can be concurrently in flight. Even if we have plenty of RAM, it is
quite likely we may hit the limit of open file descriptors our process is
allowed. Ideally, we should set some kind of bound on the active concurrency.
We might use a [semaphore](https://godoc.org/golang.org/x/sync/semaphore) or
a throttling channel:

```go
throttle := make(chan struct{}, 64)  // allow up to 64 concurrent copies
go func() {
   throttle <- struct{}{} // block until the throttle has a free slot
   defer func() { wg.Done(); <-throttle }()
   copyFile(ctx, adjusted, target, errs)
}()
```

In case you've lost count, we're up to four channels (including the context).

The lesson here is that, while Go's concurrency primitives are more than
powerful enough to express these relationships, it can be tedious to wire them
all together.

The `taskgroup` package exists to handle the plumbing for the common case of a
group of tasks that are all working on a related outcome (_e.g.,_ copying a
directory structure), and where an error on the part of any _single_ task may
be grounds for terminating the work as a whole.

The package provides a `taskgroup.Group` type that has built-in support for
some of these concerns:

- Limiting the number of active goroutines.
- Collecting and filtering errors.
- Waiting for completion and delivering status.

A `taskgroup.Group` collects error values from each task and can deliver them
to a user-provided callback. The callback can filter them or take other actions
(such as cancellation). Invocations of the callback are all done from a single
goroutine so it is safe to manipulate local resources without a lock.

A group does not directly support cancellation, but integrates cleanly with the
standard [context](https://godoc.org/context) package. A `context.CancelFunc`
can be used as a trigger to signal the whole group when an error occurs.

The API is straightforward: A task is expressed as a `func() error`, and is
added to a group using the `Go` method,

```go
g := taskgroup.New(nil).Go(myTask)
```

Any number of tasks may be added, and it is safe to do so from multiple
goroutines concurrently.  To wait for the tasks to finish, use:

```go
err := g.Wait()
```

This blocks until all the tasks in the group have returned (either
successfully, or with an error). `Wait` returns the first non-nil error
returned by any of the worker tasks.

An example program can be found in `examples/copytree/copytree.go`.

## Filtering Errors

The `taskgroup.New` function takes an optional callback that is invoked for
each non-nil error reported by a task in the group. The callback may choose
to propagate, replace, or discard the error. For example:

```go
g := taskgroup.New(func(err error) error {
   if os.IsNotExist(err) {
      return nil // ignore files that do not exist
   }
   return err
})
```

## Controlling Concurrency

The `Limit` method supports limiting the number of concurrently _active_
goroutines in the group. For example:

```go
// Allow at most 25 concurrently-active goroutines in the group.
g, start := taskgroup.New(nil).Limit(25)

// Start tasks by calling the function returned by taskgroup.Limit:
start(task1)
start(task2)
// ...
```

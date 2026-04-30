package coordinator

// Worker pool design: the coordinator owns all mutable state and builds
// closures (lambdas) that capture exactly what each worker needs. Workers
// are dumb runners that pull a job from their channel and call it. All
// completion handshakes flow back through controlMsg on controlChan, so
// the coordinator goroutine remains the single owner of refcounts,
// closedFiles, pendingPrune, etc.

// controlMsg is the union of completion messages workers send back to the
// coordinator. Implemented as a sealed interface via the unexported
// isControlMsg method.
type controlMsg interface {
	isControlMsg()
}

// readDoneMsg signals that a reader finished a job. The coordinator
// decrements inFlightReads for each path and dispatches any pendingPrune
// entries that just dropped to refcount zero.
type readDoneMsg struct {
	paths []string
}

// writerDoneMsg is reserved for an asynchronous fire-and-forget writer
// path. The synchronous awaitWriter handshake uses its own per-call
// result channel and does not flow through controlChan.
type writerDoneMsg struct{}

// pruneDoneMsg signals a pruner finished. The coordinator drops the
// matching pendingPrune entry on success.
type pruneDoneMsg struct {
	paths []string
	ok    bool
}

func (readDoneMsg) isControlMsg()   {}
func (writerDoneMsg) isControlMsg() {}
func (pruneDoneMsg) isControlMsg()  {}

// runReader is the reader worker loop. Identical shape to runWriter and
// runPruner — pulls a closure from its channel and executes it. Exits when
// the channel is closed.
func (c *Coordinator) runReader() {
	defer c.readerWG.Done()
	for job := range c.readChan {
		job()
	}
}

// runWriter is the writer worker loop. Single goroutine; the coordinator
// guarantees only one writer job is in flight at a time via awaitWriter.
func (c *Coordinator) runWriter() {
	defer c.writerWG.Done()
	for job := range c.writerChan {
		job()
	}
}

// runPruner is the pruner worker loop. Single goroutine; deletions are
// fire-and-forget from the coordinator's perspective.
func (c *Coordinator) runPruner() {
	defer c.prunerWG.Done()
	for job := range c.pruneChan {
		job()
	}
}

// dispatchRead enqueues a read closure. This may block when readChan is
// full, which in turn blocks the coordinator's run loop and therefore the
// inbound requests channel — preserving backpressure.
func (c *Coordinator) dispatchRead(job func()) {
	c.readChan <- job
}

// dispatchWrite enqueues a writer closure. Used by awaitWriter; do not
// call directly from handlers.
func (c *Coordinator) dispatchWrite(job func()) {
	c.writerChan <- job
}

// dispatchPrune enqueues a pruner closure.
func (c *Coordinator) dispatchPrune(job func()) {
	c.pruneChan <- job
}

// awaitWriter dispatches a writer job and blocks the coordinator until it
// completes. The coordinator continues to service controlChan messages
// (read/prune completions) while waiting so refcounts and pendingPrune
// stay live, but it does NOT pull new requests off c.requests — preserving
// the single-owner invariant for write state.
func (c *Coordinator) awaitWriter(job func() error) error {
	result := make(chan error, 1)
	c.dispatchWrite(func() {
		result <- job()
	})
	for {
		select {
		case err := <-result:
			return err
		case msg := <-c.controlChan:
			c.handleControl(msg)
		}
	}
}

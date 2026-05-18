package coordinator

// forcePruneTickForTest synchronously runs a prune tick and then waits for
// any dispatched pruner lambdas to complete so the test can observe the
// post-prune disk state immediately.
func forcePruneTickForTest(c *Coordinator) {
	c.handlePruneTick()
	quiesceWorkersForTest(c)
}

// bootstrapWorkersForTest initializes worker channels and spawns the
// reader/writer/pruner goroutines on a bare-constructed Coordinator.
// Tests that drive coordinator handlers directly (without going through
// New) must call this so awaitWriter and dispatchPrune don't deadlock on
// a nil channel send.
func bootstrapWorkersForTest(c *Coordinator) {
	if c.readChan != nil {
		return
	}
	if c.inFlightReads == nil {
		c.inFlightReads = make(map[string]int)
	}
	c.readChan = make(chan func(), 1024)
	c.writerChan = make(chan func(), 4)
	c.pruneChan = make(chan func(), 4)
	c.controlChan = make(chan controlMsg, 64)
	c.readWorkerCount = 2
	c.startWorkers()
}

// quiesceWorkersForTest round-trips a sentinel lambda through both the
// writer and the pruner. Because each is single-threaded, the sentinel
// runs only after every previously-dispatched lambda for that worker has
// completed. After the sentinels return, the function drains controlChan
// of any pending readDoneMsg/pruneDoneMsg so coordinator-side bookkeeping
// (refcounts, pendingPrune) reflects the just-completed work.
func quiesceWorkersForTest(c *Coordinator) {
	if c.writerChan != nil {
		done := make(chan struct{})
		c.dispatchWrite(func() { close(done) })
		<-done
	}
	if c.pruneChan != nil {
		done := make(chan struct{})
		c.dispatchPrune(func() { close(done) })
		<-done
	}
	if c.controlChan == nil {
		return
	}
	for {
		select {
		case msg := <-c.controlChan:
			c.handleControl(msg)
		default:
			return
		}
	}
}

// inFlightReadsForTest returns the active reader refcount for path.
// Callers must quiesce the workers first to avoid racing with in-flight
// readDoneMsg processing.
func inFlightReadsForTest(c *Coordinator, path string) int {
	return c.inFlightReads[path]
}

// pendingPruneCountForTest returns the number of files awaiting a refcount
// drop before being deleted.
func pendingPruneCountForTest(c *Coordinator) int {
	return len(c.pendingPrune)
}

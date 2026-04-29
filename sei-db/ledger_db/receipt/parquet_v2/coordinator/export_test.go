package coordinator

func forcePruneTickForTest(c *Coordinator) {
	c.handlePruneTick()
}

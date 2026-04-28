package parquet_v2

func forcePruneTickForTest(c *coordinator) {
	c.handlePruneTick()
}

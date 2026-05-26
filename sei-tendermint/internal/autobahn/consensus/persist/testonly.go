package persist

// SetLoadedForTest overrides loaded block data. Test-only.
func (gp *GlobalBlockPersister) SetLoadedForTest(loaded []LoadedGlobalBlock) {
	for s := range gp.state.Lock() {
		s.loaded = loaded
	}
}

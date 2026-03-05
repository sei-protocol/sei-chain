package flatcache

// A utility for scheduling asyncronous DB reads.
type readScheduler struct {
}

// Creates a new ReadScheduler.
func NewReadScheduler() *readScheduler {
	return &readScheduler{}
}

// ScheduleRead schedules a read for the given key within the given shard.
// This method returns immediately, and the read is performed asynchronously.
// When eventually completed, the read result is inserted into the provided shard entry
func (r *readScheduler) ScheduleRead(key []byte, entry *shardEntry) error {
	panic("unimplemented")
}

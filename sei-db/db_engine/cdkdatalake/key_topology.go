package cdkdatalake

// keyTopology is the store's local record of which keys the store's part of the bucket holds below the durable
// watermark. Every key lies from the scheduled deletion watermark up to but not including the durable watermark. A
// store without gaps holds every key in that range. A store with gaps holds those in keys, or keys is nil if they
// are not known.
type keyTopology struct {
	// The store's metadata.
	metadata storeMetadata

	// In a store that allows gaps, the keys in ascending order, or nil if they are not known.
	keys []uint64
}

// AllowGaps reports whether the store allows gaps between its keys.
func (t keyTopology) AllowGaps() bool {
	return t.metadata.AllowGaps()
}

// Range returns the range every key lies in.
func (t keyTopology) Range() (
	first uint64,
	last uint64,
	// False if the range is empty.
	ok bool,
) {
	first = t.metadata.ScheduledDeletionWatermark()
	end := t.metadata.DurableWatermark()
	if end <= first {
		return 0, 0, false
	}
	return first, end - 1, true
}

// Keys returns the keys of a store that allows gaps, in ascending order, or nil if they are not known.
func (t keyTopology) Keys() []uint64 {
	return t.keys
}

// IsComplete reports whether the topology says exactly which keys the store holds.
func (t keyTopology) IsComplete() bool {
	return !t.AllowGaps() || t.keys != nil
}

// Metadata returns the store's metadata.
func (t keyTopology) Metadata() storeMetadata {
	return t.metadata
}

// WithKeys returns a copy of the topology of a store that allows gaps, in which the store holds exactly keys, given
// in ascending order.
func (t keyTopology) WithKeys(keys []uint64) keyTopology {
	if keys == nil {
		keys = []uint64{}
	}
	t.keys = keys
	return t
}

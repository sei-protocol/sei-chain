package types

import "time"

// Epoch holds the complete context for a single epoch.
// Callers retrieve it from the local Registry; it is never transmitted on the wire.
type Epoch struct {
	EpochIndex uint64
	Start      RoadIndex // first RoadIndex of this epoch (inclusive)
	End        RoadIndex // last RoadIndex of this epoch (inclusive)
	Timestamp  time.Time // start time of this epoch
	Committee  *Committee
	FirstBlock GlobalBlockNumber // first global block of this epoch
}

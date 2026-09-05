package walrus

// ReadStatus reports how a historical read resolved.
type ReadStatus uint8

const (
	// ReadFound means the key held a value at the requested block.
	ReadFound ReadStatus = 0

	// ReadAbsent means the key held no value at the requested block, either because nothing wrote it at or
	// below that block or because the newest write at or below it was a deletion.
	ReadAbsent ReadStatus = 1

	// ReadTooNew means the requested block is above the newest written pod. Blocks still being accumulated
	// have no index yet, and are served by mechanisms outside this package.
	ReadTooNew ReadStatus = 2

	// ReadTooOld means the requested block fell below the retention window and the pods covering it are gone.
	ReadTooOld ReadStatus = 3
)

// String returns the name of the status.
func (s ReadStatus) String() string {
	switch s {
	case ReadFound:
		return "found"
	case ReadAbsent:
		return "absent"
	case ReadTooNew:
		return "too_new"
	case ReadTooOld:
		return "too_old"
	default:
		return "unknown"
	}
}

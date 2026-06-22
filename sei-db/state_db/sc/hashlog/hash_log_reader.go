package hashlog

// Looks for a particular block in a hash log archive. May return multiple hashes if the block
// number in question was executed multiple times (i.e. if there was a rollback).
func ReadHashForBlock(path string, block uint64) ([]*HashLog, error) {
	return nil, nil
}

// A set of hashes from two different hash log archives.
type HashLogPair struct {
	// A hash from archive A. May actually be several hashes if A contains multiple reports for the block in question.
	HashesFromA []*HashLog

	// A hash from archive B. May actually be several hashes if A contains multiple reports for the block in question.
	HashesFromB []*HashLog
}

// Compare two hash log archives, looking for differences between them. Returns information for blocks with
// deviant values. Returns blocks from lowest to highest.
func CompareHashes(
	// the path to the first archive
	pathA string,
	// the path to the second archive
	pathB string,
	// the maximum number of diffs to return, or -1 if all diffs should be returend. If nodes diverage
	// and run for a long time, the number of deviant blocks may be very large. This always returns the first
	// diffs encountered if it does not return all diffs.
	maxDiffCount int,
) ([]*HashLogPair, error) {
	return nil, nil
}

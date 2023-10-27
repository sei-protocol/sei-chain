package utils

// NextVersion get the next version
func NextVersion(v int64, initialVersion uint32) int64 {
	if v == 0 && initialVersion > 1 {
		return int64(initialVersion)
	}
	return v + 1
}

// VersionToIndex converts version to rlog index based on initial version
func VersionToIndex(version int64, initialVersion uint32) uint64 {
	if initialVersion > 1 {
		return uint64(version) - uint64(initialVersion) + 1
	}
	return uint64(version)
}

// IndexToVersion converts rlog index to version, reverse of versionToIndex
func IndexToVersion(index uint64, initialVersion uint32) int64 {
	if initialVersion > 1 {
		return int64(index) + int64(initialVersion) - 1
	}
	return int64(index)
}

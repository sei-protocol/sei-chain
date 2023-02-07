package oracle

const (
	jailCacheInterval = int64(50)
)

type JailCache struct {
	isJailed         bool
	lastUpdatedBlock int64
}

func (jailCache *JailCache) Update(currentBlockHeight int64, isJailed bool) {
	jailCache.lastUpdatedBlock = currentBlockHeight
	jailCache.isJailed = isJailed
}

func (jailCache *JailCache) IsOutdated(currentBlockHeight int64) bool {
	if currentBlockHeight < jailCacheInterval {
		return false
	}

	// This is an edge case, which should never happen.
	// The current blockchain height is lower
	// than the last updated block, to fix we should
	// just update the cached params again.
	if currentBlockHeight < jailCache.lastUpdatedBlock {
		return true
	}

	return (currentBlockHeight - jailCache.lastUpdatedBlock) > jailCacheInterval
}

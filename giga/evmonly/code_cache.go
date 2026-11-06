package evmonly

import (
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
)

// maxCachedCodes bounds how many distinct contracts a codeCache holds.
const maxCachedCodes = 4096

// codeCache holds contract bytecode by code hash, as one immutable copy shared by every block and
// transaction that loads it. It is safe for concurrent use.
type codeCache struct {
	codes sync.Map // common.Hash -> []byte
	count atomic.Int64
}

func newCodeCache() *codeCache {
	return &codeCache{}
}

// get returns the code under hash, calling load for it on a miss. load's result is copied before
// it is kept, so the caller's slice may alias store memory. Once the cache is full, a miss returns
// that copy without keeping it.
func (c *codeCache) get(hash common.Hash, load func() []byte) []byte {
	if code, ok := c.codes.Load(hash); ok {
		return code.([]byte)
	}
	code := cloneBytes(load())
	// Bytecode is addressed by its hash, so whichever goroutine stores first stored the same bytes.
	if len(code) > 0 && c.count.Load() < maxCachedCodes {
		if _, loaded := c.codes.LoadOrStore(hash, code); !loaded {
			c.count.Add(1)
		}
	}
	return code
}

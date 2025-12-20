package api

import (
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-wasmvm/types"
)

// frame stores all Iterators for one contract call
type frame []types.Iterator

// iteratorFrames contains one frame for each contract call, indexed by contract call ID.
var (
	iteratorFrames      = make(map[uint64]frame)
	iteratorFramesMutex sync.Mutex
)

// this is a global counter for creating call IDs
var (
	latestCallID      uint64
	latestCallIDMutex sync.Mutex
)

// startCall is called at the beginning of a contract call to create a new frame in iteratorFrames.
// It updates latestCallID for generating a new call ID.
func startCall() uint64 {
	latestCallIDMutex.Lock()
	defer latestCallIDMutex.Unlock()
	latestCallID += 1
	return latestCallID
}

// removeFrame removes the frame with for the given call ID.
// The result can be nil when the frame is not initialized,
// i.e. when startCall() is called but no iterator is stored.
func removeFrame(callID uint64) frame {
	iteratorFramesMutex.Lock()
	defer iteratorFramesMutex.Unlock()

	remove := iteratorFrames[callID]
	delete(iteratorFrames, callID)
	return remove
}

// endCall is called at the end of a contract call to remove one item the iteratorFrames
func endCall(callID uint64) {
	// we pull removeFrame in another function so we don't hold the mutex while cleaning up the removed frame
	remove := removeFrame(callID)
	// free all iterators in the frame when we release it
	for _, iter := range remove {
		_ = iter.Close()
	}
}

// storeIterator will add this to the end of the frame for the given ID and return a reference to it.
// We start counting with 1, so the 0 value is flagged as an error. This means we must
// remember to do idx-1 when retrieving
func storeIterator(callID uint64, it types.Iterator, frameLenLimit int) (uint64, error) {
	iteratorFramesMutex.Lock()
	defer iteratorFramesMutex.Unlock()

	oldFrameLen := len(iteratorFrames[callID])
	if oldFrameLen >= frameLenLimit {
		return 0, fmt.Errorf("reached iterator limit (%d)", frameLenLimit)
	}

	// store at array position `old_frame_len`
	iteratorFrames[callID] = append(iteratorFrames[callID], it)
	newIndex := oldFrameLen + 1
	// #nosec G115 -- checked above.
	return uint64(newIndex), nil
}

// retrieveIterator will recover an iterator based on index. This ensures it will not be garbage collected.
// We start counting with 1, in storeIterator so the 0 value is flagged as an error. This means we must
// remember to do idx-1 when retrieving
func retrieveIterator(callID uint64, index uint64) types.Iterator {
	iteratorFramesMutex.Lock()
	defer iteratorFramesMutex.Unlock()
	myFrame := iteratorFrames[callID]
	if myFrame == nil {
		return nil
	}
	if index == 0 || index > uint64(len(myFrame)) {
		// index out of range
		return nil
	}
	posInFrame := index - 1
	return myFrame[posInFrame]
}

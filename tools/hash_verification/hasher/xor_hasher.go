package hasher

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/sei-protocol/sei-db/ss/types"
)

var _ HashCalculator = (*XorHashCalculator)(nil)

// XorHashCalculator is the hash calculator backed by XoR hash.
type XorHashCalculator struct {
	NumBlocksPerWorker int64
	NumOfWorkers       int
	DataCh             chan types.RawSnapshotNode
}

// NewXorHashCalculator create a new XorHashCalculator.
func NewXorHashCalculator(numBlocksPerWorker int64, numWorkers int, data chan types.RawSnapshotNode) XorHashCalculator {
	return XorHashCalculator{
		NumBlocksPerWorker: numBlocksPerWorker,
		NumOfWorkers:       numWorkers,
		DataCh:             data,
	}
}

// HashSingle computes the hash of a single data element.
func (x XorHashCalculator) HashSingle(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

// HashTwo computes the hash of a two data elements, performs XOR between two byte slices of equal size.
func (x XorHashCalculator) HashTwo(dataA []byte, dataB []byte) []byte {
	if len(dataA) != len(dataB) {
		panic("Expecting both data to have equal length for computing a XoR hash")
	}
	result := make([]byte, len(dataA))
	for i := range dataA {
		result[i] = dataA[i] ^ dataB[i]
	}
	return result
}

func (x XorHashCalculator) ComputeHashes() [][]byte {
	var wg sync.WaitGroup
	allChannels := make([]chan types.RawSnapshotNode, x.NumOfWorkers)
	allHashes := make([][]byte, x.NumOfWorkers)
	// First calculate each sub hash in a separate goroutine
	for i := 0; i < x.NumOfWorkers; i++ {
		wg.Add(1)
		subsetChan := make(chan types.RawSnapshotNode, 1000)
		go func(index int, data chan types.RawSnapshotNode) {
			defer wg.Done()
			var hashResult []byte
			for item := range subsetChan {
				entryHash := x.HashSingle(Serialize(item))
				if hashResult == nil {
					hashResult = entryHash
				} else {
					hashResult = x.HashTwo(hashResult, entryHash)
				}
			}
			allHashes[index] = hashResult
		}(i, subsetChan)
		allChannels[i] = subsetChan
	}
	// Push all the data to its corresponding channel based on version
	for data := range x.DataCh {
		index := data.Version / x.NumBlocksPerWorker
		allChannels[index] <- data
	}
	// Close all sub channels
	for _, subChan := range allChannels {
		close(subChan)
	}
	// Wait for all workers to complete
	wg.Wait()
	// Now modify sub hashes to hash again with previous hash
	for i := 1; i < len(allHashes); i++ {
		if len(allHashes[i-1]) > 0 && len(allHashes[i]) > 0 {
			allHashes[i] = x.HashTwo(allHashes[i-1], allHashes[i])
		}
	}
	return allHashes
}

func Serialize(node types.RawSnapshotNode) []byte {
	keySize := len(node.Key)
	valueSize := len(node.Value)
	versionSize := 8
	buf := make([]byte, keySize+valueSize+versionSize)
	copy(buf[:keySize], node.Key)
	offset := keySize
	copy(buf[offset:offset+valueSize], node.Value)
	offset += valueSize
	binary.LittleEndian.PutUint64(buf[offset:offset+versionSize], uint64(node.Version))
	return buf
}

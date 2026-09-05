package walrus

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// The extension of a pod's data file.
const podExtension = ".pod"

// The extension of a pod's index sidecar.
const podIndexExtension = ".pod.idx"

// The extension of a pod's bloom filter sidecar.
const podBloomExtension = ".pod.bloom"

// The extension a pod's files carry while they are being written. A pod is renamed to its final name only
// once all three files are complete, so a file left with this extension is the wreckage of an interrupted
// build and holds nothing worth recovering.
const podPartialExtension = ".partial"

// The name shape of a pod and its sidecars: first block, last block.
var podNameRegex = regexp.MustCompile(`^(\d+)-(\d+)\.pod$`)

// Block is one block's changes, as the write path receives them.
type Block struct {
	// The block number.
	Number uint64

	// The changes the block made.
	ChangeSets []*proto.NamedChangeSet
}

// PodInfo identifies a pod by the blocks it holds.
//
// A pod's block range is recoverable from its file name alone, so listing the pods a directory holds costs a
// directory read rather than an open of every file.
type PodInfo struct {
	// The lowest block number the pod holds, inclusive.
	FirstBlock uint64

	// The highest block number the pod holds, inclusive.
	LastBlock uint64
}

// Pod is a written pod, ready to query.
//
// Its three handles hold metadata only. Every pod's handles stay resident for the life of the pod, which is
// affordable precisely because none of them pins the pod's data.
type Pod struct {
	// Which blocks the pod holds.
	Info *PodInfo

	// The pod's data file, read once the index has named an offset.
	Data PodReader

	// The pod's index, searched when the bloom filter does not rule a key out.
	Index PodIndex

	// The pod's bloom filter, tested before the index is searched.
	Bloom PodBloom
}

// Delete removes the pod's three files, joining whatever they report.
func (p *Pod) Delete() error {
	var problems []error
	if p.Data != nil {
		if err := p.Data.Delete(); err != nil {
			problems = append(problems, fmt.Errorf("failed to delete pod data file: %w", err))
		}
	}
	if p.Index != nil {
		if err := p.Index.Delete(); err != nil {
			problems = append(problems, fmt.Errorf("failed to delete pod index: %w", err))
		}
	}
	if p.Bloom != nil {
		if err := p.Bloom.Delete(); err != nil {
			problems = append(problems, fmt.Errorf("failed to delete pod bloom filter: %w", err))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("failed to delete %s: %w", p.Info, errors.Join(problems...))
}

// Size returns the bytes the pod's three files occupy together.
func (p *Pod) Size() int64 {
	return p.Data.Size() + p.Index.Size() + p.Bloom.Size()
}

// Covers reports whether the pod holds the given block.
func (p *PodInfo) Covers(blockNumber uint64) bool {
	return blockNumber >= p.FirstBlock && blockNumber <= p.LastBlock
}

// Overlaps reports whether any block the pod holds lies in the half-open range (lowBlock, highBlock].
func (p *PodInfo) Overlaps(lowBlock uint64, highBlock uint64) bool {
	return p.LastBlock > lowBlock && p.FirstBlock <= highBlock
}

// DataPath returns the path of the pod's data file within directory.
func (p *PodInfo) DataPath(directory string) string {
	return filepath.Join(directory, p.baseName()+podExtension)
}

// IndexPath returns the path of the pod's index sidecar within directory.
func (p *PodInfo) IndexPath(directory string) string {
	return filepath.Join(directory, p.baseName()+podIndexExtension)
}

// BloomPath returns the path of the pod's bloom filter sidecar within directory.
func (p *PodInfo) BloomPath(directory string) string {
	return filepath.Join(directory, p.baseName()+podBloomExtension)
}

// String returns a human readable description of the pod.
func (p *PodInfo) String() string {
	return fmt.Sprintf("pod [%d, %d]", p.FirstBlock, p.LastBlock)
}

// baseName returns the pod's name without any extension. All three of a pod's files share it.
func (p *PodInfo) baseName() string {
	return fmt.Sprintf("%d-%d", p.FirstBlock, p.LastBlock)
}

// ParsePodName reads a pod's block range out of its file name.
func ParsePodName(fileName string) (pod *PodInfo, ok bool) {
	match := podNameRegex.FindStringSubmatch(fileName)
	if match == nil {
		return nil, false
	}
	firstBlock, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return nil, false
	}
	lastBlock, err := strconv.ParseUint(match[2], 10, 64)
	if err != nil {
		return nil, false
	}
	if lastBlock < firstBlock {
		return nil, false
	}
	return &PodInfo{FirstBlock: firstBlock, LastBlock: lastBlock}, true
}

package walrus

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// The name of the data file within a pod's directory.
const podDataFileName = "data"

// The name of the hash index within a pod's directory.
const podHashIndexFileName = "hash"

// The name of the version index within a pod's directory.
const podVersionIndexFileName = "version"

// The name of the bloom filter within a pod's directory.
const podBloomFileName = "bloom"

// The extension a pod's directory carries while it is being written. The directory is renamed into place
// once every file within it is complete, so a directory left with this extension is the wreckage of an
// interrupted build and holds nothing worth recovering.
const podPartialExtension = ".partial"

// The name shape of a pod's directory: first block, last block.
var podNameRegex = regexp.MustCompile(`^(\d+)-(\d+)$`)

// Block is one block's changes, as the write path receives them.
type Block struct {
	// The block number.
	Number uint64

	// The changes the block made.
	ChangeSets []*proto.NamedChangeSet
}

// PodInfo identifies a pod by the blocks it holds.
//
// A pod's block range is recoverable from the name of its directory alone, so listing the pods an archive
// holds costs a directory read rather than an open of every file.
type PodInfo struct {
	// The lowest block number the pod holds, inclusive.
	FirstBlock uint64

	// The highest block number the pod holds, inclusive.
	LastBlock uint64
}

// Pod is a written pod, ready to query.
//
// Its handles hold metadata only. Every pod's handles stay resident for the life of the pod, which is
// affordable precisely because none of them pins the pod's data.
type Pod struct {
	// Which blocks the pod holds.
	Info *PodInfo

	// The directory holding the pod's files.
	Directory string

	// The pod's data file, read once the index has named an offset.
	Data PodReader

	// The pod's index, searched when the bloom filter does not rule a key out.
	Index PodIndex

	// The pod's bloom filter, tested before the index is searched.
	Bloom PodBloom
}

// Delete removes the pod's directory and everything in it, joining whatever the handles report.
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
	// The handles released their mappings and unlinked their own files above. What is left is the directory
	// itself, and anything an interrupted build left inside it.
	if err := os.RemoveAll(p.Directory); err != nil {
		problems = append(problems, fmt.Errorf("failed to delete pod directory %s: %w", p.Directory, err))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("failed to delete %s: %w", p.Info, errors.Join(problems...))
}

// Size returns the bytes the pod's files occupy together.
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

// DirPath returns the path of the pod's own directory within directory.
func (p *PodInfo) DirPath(directory string) string {
	return filepath.Join(directory, p.baseName())
}

// DataPath returns the path of the pod's data file within directory.
func (p *PodInfo) DataPath(directory string) string {
	return filepath.Join(p.DirPath(directory), podDataFileName)
}

// HashIndexPath returns the path of the pod's hash index within directory.
func (p *PodInfo) HashIndexPath(directory string) string {
	return filepath.Join(p.DirPath(directory), podHashIndexFileName)
}

// VersionIndexPath returns the path of the pod's version index within directory.
func (p *PodInfo) VersionIndexPath(directory string) string {
	return filepath.Join(p.DirPath(directory), podVersionIndexFileName)
}

// BloomPath returns the path of the pod's bloom filter within directory.
func (p *PodInfo) BloomPath(directory string) string {
	return filepath.Join(p.DirPath(directory), podBloomFileName)
}

// String returns a human readable description of the pod.
func (p *PodInfo) String() string {
	return fmt.Sprintf("pod [%d, %d]", p.FirstBlock, p.LastBlock)
}

// baseName returns the name of the pod's directory.
func (p *PodInfo) baseName() string {
	return fmt.Sprintf("%d-%d", p.FirstBlock, p.LastBlock)
}

// ParsePodName reads a pod's block range out of the name of its directory.
func ParsePodName(directoryName string) (pod *PodInfo, ok bool) {
	match := podNameRegex.FindStringSubmatch(directoryName)
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

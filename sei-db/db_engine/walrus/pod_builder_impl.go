package walrus

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// podShape is what one written pod holds and what it cost on disk.
type podShape struct {
	blocks       int64
	keys         int64
	entries      int64
	dataBytes    int64
	hashBytes    int64
	versionBytes int64
	bloomBytes   int64
}

var _ PodBuilder = (*podBuilder)(nil)

// podBuilder writes pods into one directory.
type podBuilder struct {
	// The archive directory each pod's own directory is created under.
	directory string

	// The rate each pod's bloom filter is sized for.
	falsePositiveRate float64

	// The instance name the builder's metrics are labeled with.
	name string
}

// Build writes the pod holding blocks and returns it open for querying.
func (b *podBuilder) Build(blocks []Block) (*Pod, error) {
	if err := checkPodBlocks(blocks); err != nil {
		return nil, err
	}
	info := &PodInfo{FirstBlock: blocks[0].Number, LastBlock: blocks[len(blocks)-1].Number}

	final := info.DirPath(b.directory)
	partial := final + podPartialExtension

	// A failure anywhere leaves only the partial directory behind, which the next open deletes. Clearing it
	// first is what lets a build retry after one that died between writing a file and publishing.
	defer func() { _ = os.RemoveAll(partial) }()
	if err := os.RemoveAll(partial); err != nil {
		return nil, fmt.Errorf("failed to clear %s: %w", partial, err)
	}
	if err := os.MkdirAll(partial, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", partial, err)
	}

	salt, err := newPodSalt()
	if err != nil {
		return nil, fmt.Errorf("failed to build %s: %w", info, err)
	}

	start := time.Now()
	refs, dataBytes, err := writePodData(filepath.Join(partial, podDataFileName), blocks, salt)
	if err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", info, err)
	}
	keyHashes, hashBytes, versionBytes, err := writePodIndex(partial, refs, info.FirstBlock, info.LastBlock, salt)
	if err != nil {
		return nil, fmt.Errorf("failed to index %s: %w", info, err)
	}
	bloomPath := filepath.Join(partial, podBloomFileName)
	bloomBytes, err := writePodBloom(bloomPath, keyHashes, b.falsePositiveRate, salt)
	if err != nil {
		return nil, fmt.Errorf("failed to build the bloom filter for %s: %w", info, err)
	}

	if err := publishPodDirectory(partial, final, b.directory); err != nil {
		return nil, fmt.Errorf("failed to publish %s: %w", info, err)
	}

	recordPodBuild(b.name, start, podShape{
		blocks:       int64(len(blocks)),
		keys:         int64(len(keyHashes)),
		entries:      int64(len(refs)),
		dataBytes:    dataBytes,
		hashBytes:    hashBytes,
		versionBytes: versionBytes,
		bloomBytes:   bloomBytes,
	})
	return openPod(b.directory, info)
}

// publishPodDirectory makes a finished pod visible, in one operation.
//
// Renaming the directory is what makes the pod appear whole or not at all. Its own entries have to be durable
// before that, since each file being synced says nothing about the directory listing them: a rename ahead of
// that sync could publish a directory a crash then leaves missing files.
func publishPodDirectory(partial string, final string, archive string) error {
	if err := util.SyncPath(partial); err != nil {
		return fmt.Errorf("failed to sync %s: %w", partial, err)
	}
	if err := os.Rename(partial, final); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", partial, final, err)
	}
	if err := util.SyncPath(archive); err != nil {
		return fmt.Errorf("failed to sync %s: %w", archive, err)
	}
	return nil
}

// newPodBuilder creates a builder writing into the configured directory.
func newPodBuilder(directory string, config *Config) *podBuilder {
	return &podBuilder{
		directory:         directory,
		falsePositiveRate: config.BloomFalsePositiveRate,
		name:              config.Name,
	}
}

// openPod opens the files in a pod's directory and returns it ready to query.
func openPod(directory string, info *PodInfo) (*Pod, error) {
	podDirectory := info.DirPath(directory)
	data, err := openPodReader(info.DataPath(directory))
	if err != nil {
		return nil, err
	}
	index, err := openPodIndex(podDirectory)
	if err != nil {
		return nil, err
	}
	bloom, err := openPodBloom(info.BloomPath(directory))
	if err != nil {
		return nil, err
	}
	if data.info.FirstBlock != info.FirstBlock || data.info.LastBlock != info.LastBlock {
		return nil, fmt.Errorf("%s holds blocks [%d, %d] but is named for [%d, %d]",
			info.DataPath(directory), data.info.FirstBlock, data.info.LastBlock,
			info.FirstBlock, info.LastBlock)
	}
	if bloom.salt != index.hashes.salt {
		return nil, fmt.Errorf("%s has a bloom filter and an index built under different salts", podDirectory)
	}
	if index.info.FirstBlock != info.FirstBlock || index.info.LastBlock != info.LastBlock {
		return nil, fmt.Errorf("%s indexes blocks [%d, %d] but is named for [%d, %d]",
			podDirectory, index.info.FirstBlock, index.info.LastBlock, info.FirstBlock, info.LastBlock)
	}
	return &Pod{Info: info, Directory: podDirectory, Data: data, Index: index, Bloom: bloom}, nil
}

// checkPodBlocks rejects a block slice a pod cannot be built from.
func checkPodBlocks(blocks []Block) error {
	if len(blocks) == 0 {
		return fmt.Errorf("a pod needs at least one block")
	}
	for index := 1; index < len(blocks); index++ {
		if blocks[index].Number != blocks[index-1].Number+1 {
			return fmt.Errorf("block %d does not follow block %d",
				blocks[index].Number, blocks[index-1].Number)
		}
	}
	return nil
}

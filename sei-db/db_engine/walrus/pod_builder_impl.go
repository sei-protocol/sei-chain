package walrus

import (
	"fmt"
	"os"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// podShape is what one written pod holds and what it cost on disk.
type podShape struct {
	blocks     int64
	keys       int64
	entries    int64
	dataBytes  int64
	indexBytes int64
	bloomBytes int64
}

var _ PodBuilder = (*podBuilder)(nil)

// podBuilder writes pods into one directory.
type podBuilder struct {
	// Where the pod's three files are written.
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

	dataPath := info.DataPath(b.directory)
	indexPath := info.IndexPath(b.directory)
	bloomPath := info.BloomPath(b.directory)
	partials := []string{
		dataPath + podPartialExtension,
		indexPath + podPartialExtension,
		bloomPath + podPartialExtension,
	}
	finals := []string{dataPath, indexPath, bloomPath}

	// A failure anywhere leaves only partials behind, which the next open deletes.
	defer func() {
		for _, partial := range partials {
			_ = os.Remove(partial)
		}
	}()

	start := time.Now()
	refs, dataBytes, err := writePodData(partials[0], blocks)
	if err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", info, err)
	}
	keys, indexBytes, err := writePodIndex(partials[1], refs, info.FirstBlock, info.LastBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to index %s: %w", info, err)
	}
	bloomBytes, err := writePodBloom(partials[2], keys, b.falsePositiveRate)
	if err != nil {
		return nil, fmt.Errorf("failed to build the bloom filter for %s: %w", info, err)
	}

	// The three files become visible together. Until the last rename lands, an interrupted build leaves only
	// partials, so a later open cannot mistake half a pod for a whole one.
	for index, partial := range partials {
		if err := os.Rename(partial, finals[index]); err != nil {
			return nil, fmt.Errorf("failed to publish %s: %w", finals[index], err)
		}
	}
	if err := util.SyncPath(b.directory); err != nil {
		return nil, fmt.Errorf("failed to sync %s after publishing %s: %w", b.directory, info, err)
	}

	recordPodBuild(b.name, start, podShape{
		blocks:     int64(len(blocks)),
		keys:       int64(len(keys)),
		entries:    int64(len(refs)),
		dataBytes:  dataBytes,
		indexBytes: indexBytes,
		bloomBytes: bloomBytes,
	})
	return openPod(b.directory, info)
}

// newPodBuilder creates a builder writing into the configured directory.
func newPodBuilder(directory string, config *Config) *podBuilder {
	return &podBuilder{
		directory:         directory,
		falsePositiveRate: config.BloomFalsePositiveRate,
		name:              config.Name,
	}
}

// openPod opens a pod's three files and returns it ready to query.
func openPod(directory string, info *PodInfo) (*Pod, error) {
	data, err := openPodReader(info.DataPath(directory))
	if err != nil {
		return nil, err
	}
	index, err := openPodIndex(info.IndexPath(directory))
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
	return &Pod{Info: info, Data: data, Index: index, Bloom: bloom}, nil
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

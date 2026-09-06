package walrus

import (
	"fmt"
	"regexp"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// The permitted shape of an instance name: it becomes a metric attribute value, so it is restricted to
// characters safe for label values.
var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// maxPodDataSize is the largest a pod's data section may be. A pod index addresses an entry by a uint32 byte
// offset into that section, so a pod that grew past this would produce unreachable entries.
const maxPodDataSize = 4 * unit.GB

// Config configures a Walrus instance.
type Config struct {
	// The directory where the instance writes its pods, indexes, bloom filters, and retained snapshots.
	Path string

	// A short identifier for this instance, used to distinguish its metrics from those of other instances in
	// the same process. Required; must match [a-zA-Z0-9_-]+.
	Name string

	// The name of the store whose changesets this instance indexes. AppendBlock rejects a changeset carrying
	// any other name.
	StoreName string

	// The size a pod's data section may reach before the accumulator cuts the pod. A block is never split
	// across pods, so a block that does not fit in what remains starts a new pod instead. Must be greater
	// than 0 and no larger than 4 GiB.
	TargetPodSize uint64

	// The false positive rate each pod's bloom filter is sized for. Must be greater than 0 and less than 1.
	//
	// This trades bloom filter bytes against wasted pod opens: a walk that gets a false positive pays a pod
	// index search to learn the key is not there.
	BloomFalsePositiveRate float64

	// The number of blocks below the newest appended block that stay queryable. Pods and snapshots below the
	// window are deleted, except the newest snapshot at or below the window's floor. Must be greater than 0.
	//
	// This is what sets how much disk the engine holds: the window multiplied by what a block costs, which
	// is the entry bytes plus the index and filter built over them. The default is deliberately small enough
	// to be safe on any host, because a retention window is a decision about a machine's capacity and a
	// default that silently implies terabytes is a trap rather than a convenience.
	RetentionBlocks uint64

	// The number of pods that may be built at the same time.
	//
	// A build holds its whole pod in memory along with the buffers it sorts, so this is bounded by memory
	// rather than by cores: at the default pod size two concurrent builds want roughly 15 GiB. Must be
	// greater than 0.
	PodBuildConcurrency int

	// Whether this instance stops recording metrics. Metrics are on by default, including for a zero-valued
	// Config, so a caller has to ask for silence rather than remember to ask for data.
	//
	// Set this only when Name is not unique among instances that are live at the same time: every instrument
	// is labeled by Name alone, so such instances overwrite each other's samples.
	DisableMetrics bool
}

// DefaultConfig returns a default configuration for the instance at path, identified by name, indexing the
// store named storeName.
func DefaultConfig(path string, name string, storeName string) *Config {
	return &Config{
		Path:                   path,
		Name:                   name,
		StoreName:              storeName,
		TargetPodSize:          4 * unit.GB,
		BloomFalsePositiveRate: 0.01,
		RetentionBlocks:        100_000,
		PodBuildConcurrency:    2,
		DisableMetrics:         false,
	}
}

// Validate the configuration, returning nil if valid, or an error describing the problem if invalid.
func (c *Config) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("path is required")
	}
	if !nameRegex.MatchString(c.Name) {
		return fmt.Errorf("name %q is required and must match %s", c.Name, nameRegex.String())
	}
	if c.StoreName == "" {
		return fmt.Errorf("store name is required")
	}
	if c.TargetPodSize == 0 {
		// A zero target would cut a fresh pod after every single block.
		return fmt.Errorf("target pod size must be greater than 0")
	}
	if c.TargetPodSize > maxPodDataSize {
		return fmt.Errorf("target pod size %d exceeds the addressable maximum of %d",
			c.TargetPodSize, uint64(maxPodDataSize))
	}
	if c.BloomFalsePositiveRate <= 0 || c.BloomFalsePositiveRate >= 1 {
		return fmt.Errorf("bloom false positive rate must be in (0, 1), got %v", c.BloomFalsePositiveRate)
	}
	if c.RetentionBlocks == 0 {
		// A zero window would delete every pod as soon as it was sealed.
		return fmt.Errorf("retention blocks must be greater than 0")
	}
	if c.PodBuildConcurrency <= 0 {
		return fmt.Errorf("pod build concurrency must be greater than 0")
	}
	return nil
}

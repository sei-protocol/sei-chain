package walrus

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// testBlock builds a block from a flat list of key/value/delete triples.
func testBlock(number uint64, pairs ...*proto.KVPair) Block {
	return Block{
		Number:     number,
		ChangeSets: []*proto.NamedChangeSet{{Name: "evm", Changeset: proto.ChangeSet{Pairs: pairs}}},
	}
}

// testPair builds one key change.
func testPair(key string, value string, deleted bool) *proto.KVPair {
	return &proto.KVPair{Key: []byte(key), Value: []byte(value), Delete: deleted}
}

// oracle is a brute force model of what a pod should answer, built from the same blocks the pod was.
type oracle map[string][]oracleVersion

// oracleVersion is one version of one key.
type oracleVersion struct {
	block   uint64
	value   string
	deleted bool
}

// newOracle records every version of every key the blocks wrote, last write of a block winning.
func newOracle(blocks []Block) oracle {
	model := oracle{}
	for _, block := range blocks {
		for _, changeSet := range block.ChangeSets {
			for _, pair := range changeSet.Changeset.Pairs {
				versions := model[string(pair.Key)]
				version := oracleVersion{
					block:   block.Number,
					value:   string(pair.Value),
					deleted: pair.Delete,
				}
				if len(versions) > 0 && versions[len(versions)-1].block == block.Number {
					versions[len(versions)-1] = version
				} else {
					versions = append(versions, version)
				}
				model[string(pair.Key)] = versions
			}
		}
	}
	return model
}

// newest returns the version of key in the half open block range (lowBlock, highBlock].
func (o oracle) newest(key string, lowBlock uint64, highBlock uint64) (oracleVersion, bool) {
	var best oracleVersion
	found := false
	for _, version := range o[key] {
		if version.block > highBlock {
			break
		}
		if version.block <= lowBlock {
			continue
		}
		best = version
		found = true
	}
	return best, found
}

func TestPodRoundTrip(t *testing.T) {
	directory := t.TempDir()

	// A mix that exercises the interesting shapes: a key written every block, keys written once, keys sharing
	// an eight byte prefix so level one ties have to be broken on the whole key, a key written twice in one
	// block, and a deletion.
	blocks := make([]Block, 0, 40)
	for number := uint64(100); number < 140; number++ {
		pairs := []*proto.KVPair{
			testPair("hot", fmt.Sprintf("hot-%d", number), false),
			testPair(fmt.Sprintf("cold-%06d", number), fmt.Sprintf("cold-%d", number), false),
			testPair(fmt.Sprintf("sameprefix-%d", number%3), fmt.Sprintf("shared-%d", number), false),
		}
		if number%7 == 0 {
			pairs = append(pairs, testPair("hot", fmt.Sprintf("hot-%d-again", number), false))
		}
		if number%11 == 0 {
			pairs = append(pairs, testPair("doomed", "", true))
		} else {
			pairs = append(pairs, testPair("doomed", fmt.Sprintf("alive-%d", number), false))
		}
		blocks = append(blocks, testBlock(number, pairs...))
	}

	config := DefaultConfig(directory, "test", "evm")
	pod, err := newPodBuilder(directory, config).Build(blocks)
	require.NoError(t, err)

	require.Equal(t, uint64(100), pod.Info.FirstBlock)
	require.Equal(t, uint64(139), pod.Info.LastBlock)

	model := newOracle(blocks)

	// Every key the pod holds must be found by the bloom filter: false negatives are not permitted.
	for key := range model {
		require.True(t, pod.Bloom.MayContain([]byte(key)), "bloom filter lost key %q", key)
	}

	// Every key, at every block in and around the pod's range, against the brute force model.
	for key := range model {
		for highBlock := uint64(95); highBlock <= 145; highBlock++ {
			for _, lowBlock := range []uint64{0, 99, 110, 120, 139} {
				if lowBlock >= highBlock {
					continue
				}
				offset, block, found, present, err := pod.Index.FindNewest(
					[]byte(key), lowBlock, highBlock)
				require.NoError(t, err)
				require.True(t, present, "the pod holds key %q, so the index must say so", key)

				want, wantFound := model.newest(key, lowBlock, min(highBlock, 139))
				require.Equal(t, wantFound, found, "key %q in (%d, %d]", key, lowBlock, highBlock)
				if !wantFound {
					continue
				}
				require.Equal(t, want.block, block, "key %q in (%d, %d]", key, lowBlock, highBlock)

				value, deleted, err := pod.Data.ReadEntry(offset)
				require.NoError(t, err)
				require.Equal(t, want.deleted, deleted, "key %q at block %d", key, block)
				require.Equal(t, want.value, string(value), "key %q at block %d", key, block)
			}
		}
	}

	// A key the pod never held is never found, whatever the bloom filter says about it.
	for _, absent := range []string{"missing", "hot!", "cold-999999", "sameprefix-9"} {
		_, _, found, present, err := pod.Index.FindNewest([]byte(absent), 0, 200)
		require.NoError(t, err)
		require.False(t, found, "key %q should not be in the pod", absent)
		require.False(t, present, "key %q is not in the pod, so it is a bloom false positive", absent)
	}
}

func TestPodReopen(t *testing.T) {
	directory := t.TempDir()
	blocks := []Block{
		testBlock(7, testPair("alpha", "one", false)),
		testBlock(8, testPair("beta", "two", false)),
		testBlock(9, testPair("alpha", "three", false)),
	}

	config := DefaultConfig(directory, "test", "evm")
	built, err := newPodBuilder(directory, config).Build(blocks)
	require.NoError(t, err)

	reopened, err := openPod(directory, built.Info)
	require.NoError(t, err)

	offset, block, found, present, err := reopened.Index.FindNewest([]byte("alpha"), 0, 9)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, present)
	require.Equal(t, uint64(9), block)

	value, deleted, err := reopened.Data.ReadEntry(offset)
	require.NoError(t, err)
	require.False(t, deleted)
	require.Equal(t, "three", string(value))

	require.Equal(t, built.Data.Size(), reopened.Data.Size())
	require.Positive(t, reopened.Size())
}

func TestPodBuildLeavesNoPartials(t *testing.T) {
	directory := t.TempDir()
	config := DefaultConfig(directory, "test", "evm")

	pod, err := newPodBuilder(directory, config).Build([]Block{testBlock(1, testPair("k", "v", false))})
	require.NoError(t, err)

	// A build publishes one directory, and nothing of the one it was assembled in survives.
	entries, err := listDirectory(directory)
	require.NoError(t, err)
	require.Equal(t, []string{"1-1"}, entries, "a pod is exactly one directory: %v", entries)

	files, err := listDirectory(pod.Directory)
	require.NoError(t, err)
	require.Equal(t, []string{
		podBloomFileName, podDataFileName, podHashIndexFileName, podVersionIndexFileName,
	}, files)
}

func TestPodBuildClearsAnInterruptedBuild(t *testing.T) {
	directory := t.TempDir()
	config := DefaultConfig(directory, "test", "evm")

	// A build that died before publishing leaves its directory behind under the partial name. The next build
	// of the same pod has to clear it rather than write into what it left.
	info := &PodInfo{FirstBlock: 1, LastBlock: 1}
	partial := info.DirPath(directory) + podPartialExtension
	require.NoError(t, os.MkdirAll(partial, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(partial, podDataFileName), []byte("wreckage"), 0o600))

	pod, err := newPodBuilder(directory, config).Build([]Block{testBlock(1, testPair("k", "v", false))})
	require.NoError(t, err)

	entries, err := listDirectory(directory)
	require.NoError(t, err)
	require.Equal(t, []string{"1-1"}, entries, "the interrupted build should be gone: %v", entries)

	offset, _, found, _, err := pod.Index.FindNewest([]byte("k"), 0, 100)
	require.NoError(t, err)
	require.True(t, found, "the rebuilt pod should be queryable")

	value, _, err := pod.Data.ReadEntry(offset)
	require.NoError(t, err)
	require.Equal(t, "v", string(value))
}

func TestPodRejectsNonContiguousBlocks(t *testing.T) {
	directory := t.TempDir()
	config := DefaultConfig(directory, "test", "evm")

	_, err := newPodBuilder(directory, config).Build([]Block{
		testBlock(1, testPair("k", "v", false)),
		testBlock(3, testPair("k", "v", false)),
	})
	require.ErrorContains(t, err, "does not follow")

	_, err = newPodBuilder(directory, config).Build(nil)
	require.ErrorContains(t, err, "at least one block")
}

// TestPodIndexSeparatesPresenceFromRange pins the distinction the bloom filter's error rate depends on.
//
// A pod can hold a key whose every version falls outside the queried range. The bloom filter that admitted
// that pod was right, and counting it as a false positive would overstate the filter's error rate — which is
// the number the whole schema is being judged on.
func TestPodIndexSeparatesPresenceFromRange(t *testing.T) {
	directory := t.TempDir()
	blocks := []Block{
		testBlock(10, testPair("early", "value", false)),
		testBlock(11, testPair("filler", "value", false)),
		testBlock(12, testPair("filler", "value", false)),
	}

	config := DefaultConfig(directory, "test", "evm")
	pod, err := newPodBuilder(directory, config).Build(blocks)
	require.NoError(t, err)

	// "early" was written at block 10, so a walk over (10, 12] finds nothing — but the pod does hold it.
	_, _, found, present, err := pod.Index.FindNewest([]byte("early"), 10, 12)
	require.NoError(t, err)
	require.False(t, found, "no version of the key lies above the floor")
	require.True(t, present, "the pod holds the key, so this is not a bloom false positive")

	// The same key over a range that does include its write is found.
	_, block, found, present, err := pod.Index.FindNewest([]byte("early"), 9, 12)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, present)
	require.Equal(t, uint64(10), block)

	// A key the pod never held is absent both ways, which is what a real false positive looks like.
	_, _, found, present, err = pod.Index.FindNewest([]byte("never"), 0, 12)
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, present)
}

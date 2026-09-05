package walrus

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildTestPod writes a pod covering the given block range, one key per block.
func buildTestPod(t *testing.T, directory string, firstBlock uint64, lastBlock uint64) *Pod {
	t.Helper()

	blocks := make([]Block, 0, lastBlock-firstBlock+1)
	for number := firstBlock; number <= lastBlock; number++ {
		blocks = append(blocks, testBlock(number, testPair("key", "value", false)))
	}
	pod, err := newPodBuilder(directory, DefaultConfig(directory, "test", "evm")).Build(blocks)
	require.NoError(t, err)
	return pod
}

// stubSnapshot is a snapshot with nothing behind it, for exercising the catalog's bookkeeping.
type stubSnapshot struct {
	block   uint64
	deleted bool
}

func (s *stubSnapshot) BlockNumber() uint64                              { return s.block }
func (s *stubSnapshot) Get(_ []byte) (value []byte, found bool, e error) { return nil, false, nil }
func (s *stubSnapshot) Path() string                                     { return "stub" }
func (s *stubSnapshot) Size() int64                                      { return 1 }

func (s *stubSnapshot) Delete() error {
	s.deleted = true
	return nil
}

func TestCatalogKeepsThePseudoSnapshotAsTheFloorUntilARealOneArrives(t *testing.T) {
	directory := t.TempDir()
	config := DefaultConfig(directory, "test", "evm")
	config.DisableMetrics = true

	catalog := newCatalog(config, []*Pod{buildTestPod(t, directory, 1, 50)}, nil)

	// However far the query floor is raised, a pod cannot go while the only floor is the pseudo-snapshot:
	// a walk falling into that gap would answer absent for a key that had a value.
	catalog.SetQueryFloor(10_000)
	files, _, err := catalog.Collect()
	require.NoError(t, err)
	require.Zero(t, files)

	ok, first, last := catalog.Bounds()
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(50), last)
}

func TestCatalogAdvancesTheFloorOntoARealSnapshot(t *testing.T) {
	directory := t.TempDir()
	config := DefaultConfig(directory, "test", "evm")
	config.DisableMetrics = true

	pods := []*Pod{
		buildTestPod(t, directory, 1, 50),
		buildTestPod(t, directory, 51, 100),
		buildTestPod(t, directory, 101, 150),
	}
	snapshot := &stubSnapshot{block: 60}
	catalog := newCatalog(config, pods, []Snapshot{snapshot})

	catalog.SetQueryFloor(120)
	files, bytes, err := catalog.Collect()
	require.NoError(t, err)

	// The floor lands on block 60, so only the pod entirely below it goes. The pod straddling 60 is kept
	// whole, because the blocks above 60 in it are still reachable.
	require.Equal(t, 3, files, "one pod is three files")
	require.Positive(t, bytes)
	require.Equal(t, uint64(60), catalog.floorBlock)

	ok, first, _ := catalog.Bounds()
	require.True(t, ok)
	require.Equal(t, uint64(51), first)
	require.False(t, snapshot.deleted, "the floor snapshot itself must survive")
}

func TestCatalogWillNotDeleteWhatAQueryIsReading(t *testing.T) {
	directory := t.TempDir()
	config := DefaultConfig(directory, "test", "evm")
	config.DisableMetrics = true

	pods := []*Pod{
		buildTestPod(t, directory, 1, 50),
		buildTestPod(t, directory, 51, 100),
	}
	catalog := newCatalog(config, pods, []Snapshot{&stubSnapshot{block: 60}})

	// A query admitted before the floor moved still has to finish reading what it resolved.
	query, admitted := catalog.Query(40)
	require.True(t, admitted)
	require.Equal(t, uint64(40), query.Block())

	catalog.SetQueryFloor(120)
	files, _, err := catalog.Collect()
	require.NoError(t, err)
	require.Zero(t, files, "a referenced pod was deleted out from under a query")
	require.FileExists(t, pods[0].Data.Path())

	query.Release()

	files, _, err = catalog.Collect()
	require.NoError(t, err)
	require.Equal(t, 3, files)
	require.NoFileExists(t, pods[0].Data.Path())
}

func TestCatalogQueryResolvesTheWalkAndRefusesBelowTheFloor(t *testing.T) {
	directory := t.TempDir()
	config := DefaultConfig(directory, "test", "evm")
	config.DisableMetrics = true

	pods := []*Pod{
		buildTestPod(t, directory, 1, 50),
		buildTestPod(t, directory, 51, 100),
		buildTestPod(t, directory, 101, 150),
	}
	catalog := newCatalog(config, pods, []Snapshot{&stubSnapshot{block: 60}})

	// A walk from block 120 covers the pods above the floor at 60 and stops there: the pod ending at 50 is
	// entirely below the floor, so the snapshot already accounts for it.
	query, admitted := catalog.Query(120)
	require.True(t, admitted)
	require.Equal(t, uint64(60), query.Floor().BlockNumber())

	walked := query.Pods()
	require.Len(t, walked, 2)
	require.Equal(t, uint64(101), walked[0].Info.FirstBlock, "a walk visits the newest pod first")
	require.Equal(t, uint64(51), walked[1].Info.FirstBlock)
	query.Release()

	catalog.SetQueryFloor(70)
	_, admitted = catalog.Query(65)
	require.False(t, admitted, "a query below the floor should be refused, not answered")
}

func TestCatalogPanicsOnAPodOutOfOrder(t *testing.T) {
	directory := t.TempDir()
	config := DefaultConfig(directory, "test", "evm")
	config.DisableMetrics = true

	catalog := newCatalog(config, []*Pod{buildTestPod(t, directory, 1, 50)}, nil)
	skipped := buildTestPod(t, directory, 101, 150)

	// A gap is a fault in the orderer upstream, not something a caller can be asked to handle: leaving one
	// would let a walk fall through it and answer from the floor.
	require.Panics(t, func() { catalog.AddPod(skipped) })
}

func TestPodInfoPathsAndParsing(t *testing.T) {
	info := &PodInfo{FirstBlock: 12, LastBlock: 34}
	require.Equal(t, filepath.Join("d", "12-34.pod"), info.DataPath("d"))
	require.Equal(t, filepath.Join("d", "12-34.pod.idx"), info.IndexPath("d"))
	require.Equal(t, filepath.Join("d", "12-34.pod.bloom"), info.BloomPath("d"))

	parsed, ok := ParsePodName("12-34.pod")
	require.True(t, ok)
	require.Equal(t, info.FirstBlock, parsed.FirstBlock)
	require.Equal(t, info.LastBlock, parsed.LastBlock)

	for _, bad := range []string{"12-34.pod.idx", "34-12.pod", "12.pod", "pod", "12-34.pod.partial"} {
		_, ok := ParsePodName(bad)
		require.False(t, ok, "%q should not parse as a pod name", bad)
	}

	require.True(t, info.Covers(12) && info.Covers(34) && !info.Covers(35))
	require.True(t, info.Overlaps(11, 12))
	require.False(t, info.Overlaps(34, 100), "a range starting at the last block excludes the whole pod")
}

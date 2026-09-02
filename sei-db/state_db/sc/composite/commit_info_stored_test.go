package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// storedInfoConfig is a mode where both backends contribute to the commit info, which is the only
// configuration in which a combined value could straddle two blocks.
func storedInfoConfig() config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.EVMMigrated
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cfg.MemIAVLConfig.SnapshotKeepRecent = 1000
	return cfg
}

func openStoredInfoStore(t *testing.T, dir string) *CompositeCommitStore {
	t.Helper()
	cs, err := NewCompositeCommitStore(t.Context(), dir, storedInfoConfig(), nil)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	require.NoError(t, cs.LoadLatest())
	return cs
}

func storedInfoChangeset(value byte) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{
		{
			Name: keys.BankStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte("key"), Value: []byte{value}}},
			},
		},
	}
}

// latticeEntry returns the evm_lattice store info, which carries flatkv's contribution.
func latticeEntry(t *testing.T, ci *proto.CommitInfo) proto.StoreInfo {
	t.Helper()
	for _, si := range ci.StoreInfos {
		if si.Name == "evm_lattice" {
			return si
		}
	}
	t.Fatalf("commit info has no evm_lattice entry: %+v", ci.StoreInfos)
	return proto.StoreInfo{}
}

func commitStoredInfoBlock(t *testing.T, cs *CompositeCommitStore, value byte) int64 {
	t.Helper()
	require.NoError(t, cs.ApplyChangeSets(storedInfoChangeset(value)))
	building := cs.Version() + 1
	require.NotNil(t, cs.WorkingCommitInfo(building))
	_, err := cs.Commit(building)
	require.NoError(t, err)
	return building
}

// TestLastCommitInfoUnmovedByWorkingHash is the case the stored value exists for. Taking a block's
// working hash seals it on flatkv and destroys flatkv's previous hash, so a commit info rebuilt from the
// backends at that moment would carry the new block's lattice hash beside the old block's memiavl
// hashes. The stored value still describes the block that was actually committed.
func TestLastCommitInfoUnmovedByWorkingHash(t *testing.T) {
	cs := openStoredInfoStore(t, t.TempDir())
	defer func() { require.NoError(t, cs.Close()) }()

	committed := commitStoredInfoBlock(t, cs, 1)

	before := cs.LastCommitInfo()
	require.NotNil(t, before)
	require.Equal(t, committed, before.Version)

	// Seal the next block on flatkv without committing it.
	require.NoError(t, cs.ApplyChangeSets(storedInfoChangeset(2)))
	require.NotNil(t, cs.WorkingCommitInfo(cs.Version()+1))

	flatKVVersion := cs.flatKV.Version()
	require.Equal(t, committed+1, flatKVVersion, "flatkv should be a block ahead for this test to mean anything")

	after := cs.LastCommitInfo()
	require.Equal(t, committed, after.Version, "last commit info must not follow flatkv's seal")
	require.Equal(t, before.StoreInfos, after.StoreInfos, "no store info may change without a commit")
	require.Equal(t, committed, latticeEntry(t, after).CommitId.Version)
}

// TestLastCommitInfoSurvivesReopen covers the case a commit-time-only refresh would miss: a store opened
// at a committed height has a commit info, but no commit ran in this process to populate it.
func TestLastCommitInfoSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	cs := openStoredInfoStore(t, dir)

	committed := commitStoredInfoBlock(t, cs, 1)
	before := cs.LastCommitInfo()
	require.NoError(t, cs.Close())

	reopened := openStoredInfoStore(t, dir)
	defer func() { require.NoError(t, reopened.Close()) }()

	after := reopened.LastCommitInfo()
	require.NotNil(t, after, "a store opened at a committed height must report its commit info")
	require.Equal(t, committed, after.Version)
	require.Equal(t, before, after, "reopening at the same height must report the same commit info")
}

// TestLastCommitInfoAfterRollback asserts the rebuilt value matches what a store that committed straight
// to that height reports, which is what makes the hash `seid rollback` prints trustworthy.
func TestLastCommitInfoAfterRollback(t *testing.T) {
	target := int64(2)

	reference := openStoredInfoStore(t, t.TempDir())
	for v := byte(1); int64(v) <= target; v++ {
		commitStoredInfoBlock(t, reference, v)
	}
	want := reference.LastCommitInfo()
	require.NoError(t, reference.Close())

	cs := openStoredInfoStore(t, t.TempDir())
	defer func() { require.NoError(t, cs.Close()) }()
	for v := byte(1); int64(v) <= target+2; v++ {
		commitStoredInfoBlock(t, cs, v)
	}
	require.NoError(t, cs.Rollback(target))

	got := cs.LastCommitInfo()
	require.NotNil(t, got)
	require.Equal(t, target, got.Version)
	requireCommitInfoEqual(t, want, got, "post-rollback commit info")
}

// TestLastCommitInfoOnReadOnlyHandle covers the handle rootmulti's historical proof path queries. An
// unpopulated handle reports nil, and that path silently drops the commit proof rather than failing.
func TestLastCommitInfoOnReadOnlyHandle(t *testing.T) {
	cs := openStoredInfoStore(t, t.TempDir())
	defer func() { require.NoError(t, cs.Close()) }()

	commitStoredInfoBlock(t, cs, 1)
	target := commitStoredInfoBlock(t, cs, 2)
	commitStoredInfoBlock(t, cs, 3)

	handle, err := cs.LoadVersionReadOnly(target)
	require.NoError(t, err)
	defer func() { require.NoError(t, handle.Close()) }()

	info := handle.LastCommitInfo()
	require.NotNil(t, info, "a read-only handle must report the commit info at the version it opened")
	require.Equal(t, target, info.Version)
	require.Equal(t, target, latticeEntry(t, info).CommitId.Version)
}

// TestLastCommitInfoIsACopy guards the accessor: a caller that mutates what it got back must not corrupt
// what the next caller sees.
func TestLastCommitInfoIsACopy(t *testing.T) {
	cs := openStoredInfoStore(t, t.TempDir())
	defer func() { require.NoError(t, cs.Close()) }()

	commitStoredInfoBlock(t, cs, 1)

	first := cs.LastCommitInfo()
	require.NotEmpty(t, first.StoreInfos)
	require.NotEmpty(t, first.StoreInfos[0].CommitId.Hash, "need a hash to test aliasing against")

	wantVersion := first.Version
	wantName := first.StoreInfos[0].Name
	wantHash := append([]byte(nil), first.StoreInfos[0].CommitId.Hash...)

	first.Version = 9999
	first.StoreInfos[0].Name = "clobbered"
	// The hash is the one that would alias without a per-element copy: the slice header is copied by a
	// plain slice copy, but the bytes behind it are not.
	first.StoreInfos[0].CommitId.Hash[0] ^= 0xFF

	second := cs.LastCommitInfo()
	require.Equal(t, wantVersion, second.Version)
	require.Equal(t, wantName, second.StoreInfos[0].Name)
	require.Equal(t, wantHash, second.StoreInfos[0].CommitId.Hash)
}

package migration

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/stretchr/testify/require"
)

// countingFlatKV decorates a flatkv.Store, counting ApplyChangeSets calls so a
// test can assert how many physical flatKV writes a single dispatch produces.
// All other methods are inherited from the embedded Store.
type countingFlatKV struct {
	flatkv.Store
	applyCount int
}

func (c *countingFlatKV) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	c.applyCount++
	return c.Store.ApplyChangeSets(cs)
}

// TestBuildRouter_FlatKVWrittenOncePerDispatch asserts that a single top-level
// ApplyChangeSets produces exactly one physical flatKV ApplyChangeSets, even in
// the modes where two routes fan out to flatKV (MigrateAllButBank: evm/ direct
// + migration manager; MigrateBank: all-but-bank direct + migration manager).
// EVMMigrated is included as a control (a single flatKV route).
func TestBuildRouter_FlatKVWrittenOncePerDispatch(t *testing.T) {
	rng := testutil.NewTestRandom()

	// A module handled by the migration manager / non-evm-non-bank routes.
	otherModules, err := keys.AllModulesExcept(keys.EVMStoreKey, keys.BankStoreKey)
	require.NoError(t, err)
	require.NotEmpty(t, otherModules)
	otherModule := otherModules[0]

	cases := []struct {
		name string
		mode config.WriteMode
	}{
		{"MigrateAllButBank", config.MigrateAllButBank},
		{"MigrateBank", config.MigrateBank},
		{"EVMMigrated", config.EVMMigrated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			memiavlDB := NewTestMemIAVLCommitStore(t, t.TempDir(), keys.MemIAVLStoreKeys)
			flatKVDB := NewTestFlatKVCommitStore(t, t.TempDir())
			counting := &countingFlatKV{Store: flatKVDB}

			router, err := BuildRouter(t.Context(), tc.mode, memiavlDB, counting, 100)
			require.NoError(t, err)

			cs := []*proto.NamedChangeSet{
				namedCS(keys.EVMStoreKey, randomEVMKVPair(rng)),
				namedCS(keys.BankStoreKey, kv("bk", "bv")),
				namedCS(otherModule, kv("ok", "ov")),
			}
			require.NoError(t, router.ApplyChangeSets(cs, true))

			require.Equal(t, 1, counting.applyCount,
				"a single dispatch must produce exactly one flatKV ApplyChangeSets")
		})
	}
}

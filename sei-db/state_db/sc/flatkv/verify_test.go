package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// TestVerifyDBModuleMetadataOrphanStats ensures a stats entry for a module
// with no on-disk keys and no maintained hash cannot slip past verification.
// The hash-keyed residue loop alone would miss it.
func TestVerifyDBModuleMetadataOrphanStats(t *testing.T) {
	cs := &CommitStore{
		committedVersion:         1,
		perDBWorkingLtHash:       map[string]*lthash.LtHash{storageDBDir: lthash.New()},
		perDBModuleWorkingLtHash: map[string]map[string]*lthash.LtHash{storageDBDir: {}},
		perDBModuleWorkingStats: map[string]map[string]lthash.ModuleStats{
			storageDBDir: {
				"orphan": {KeyCount: 3, Bytes: 99},
			},
		},
	}

	_, err := cs.verifyDBModuleMetadata(storageDBDir, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "per-module stats")
	require.Contains(t, err.Error(), "orphan")
}

func TestVerifyDBModuleMetadataZeroResidueOK(t *testing.T) {
	cs := &CommitStore{
		committedVersion: 1,
		perDBWorkingLtHash: map[string]*lthash.LtHash{
			storageDBDir: lthash.New(),
		},
		perDBModuleWorkingLtHash: map[string]map[string]*lthash.LtHash{
			storageDBDir: {"gone": lthash.New()},
		},
		perDBModuleWorkingStats: map[string]map[string]lthash.ModuleStats{
			storageDBDir: {"gone": {}},
		},
	}

	root, err := cs.verifyDBModuleMetadata(storageDBDir, nil, nil)
	require.NoError(t, err)
	require.True(t, root.IsZero())
}

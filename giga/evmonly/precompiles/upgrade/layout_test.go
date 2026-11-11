package upgrade

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// forgeLayout is `forge inspect UpgradeStorage storage-layout --json`.
type forgeLayout struct {
	Storage []forgeEntry `json:"storage"`
	Types   map[string]struct {
		Members []forgeEntry `json:"members"`
	} `json:"types"`
}

type forgeEntry struct {
	Label  string `json:"label"`
	Offset int    `json:"offset"`
	Slot   string `json:"slot"`
}

// TestLayoutMatchesForge pins every slot to the compiler's layout of
// docs/giga-storage/UpgradeStorage.sol. Regenerate testdata with
// `forge inspect UpgradeStorage storage-layout --json` after editing the schema.
func TestLayoutMatchesForge(t *testing.T) {
	bz, err := os.ReadFile("testdata/upgrade_storage_layout.json")
	require.NoError(t, err)
	var layout forgeLayout
	require.NoError(t, json.Unmarshal(bz, &layout))

	got := map[string]uint64{}
	for _, e := range layout.Storage {
		require.Zero(t, e.Offset, e.Label)
		got[e.Label] = mustUint(t, e.Slot)
	}
	require.Equal(t, map[string]uint64{"plan": slotPlan, "appliedHeight": slotAppliedHeight, "lastApplied": slotLastApplied}, got)

	seen := 0
	for typeID, typ := range layout.Types {
		if !strings.HasPrefix(typeID, "t_struct(Plan)") {
			continue
		}
		members := map[string]uint64{}
		for _, e := range typ.Members {
			require.Zero(t, e.Offset, e.Label)
			members[e.Label] = mustUint(t, e.Slot)
		}
		require.Equal(t, map[string]uint64{"name": planName, "height": planHeight, "info": planInfo, "__gap": planGap}, members)
		seen++
	}
	require.Equal(t, 1, seen)
}

func mustUint(t *testing.T, s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	require.NoError(t, err)
	return v
}

// TestReadsCompilerWrites runs UpgradeStorageHarness, which writes
// UpgradeStorage with solc's codegen, and reads the result back. Rebuild
// testdata with `forge inspect UpgradeStorageHarness deployedBytecode` and the
// remapping giga-storage/=docs/giga-storage/.
func TestReadsCompilerWrites(t *testing.T) {
	code, err := os.ReadFile("testdata/UpgradeStorageHarness.bin-runtime")
	require.NoError(t, err)
	seed := common.FromHex("0x7d94792a") // seed()
	_, db, err := runtime.Execute(common.FromHex(strings.TrimSpace(string(code))), seed, &runtime.Config{GasLimit: 10_000_000})
	require.NoError(t, err)
	// runtime.Execute deploys the code at the zero-padded address of "contract".
	contract := common.BytesToAddress([]byte("contract"))
	state := newMemState()
	for _, slot := range []common.Hash{planSlot(planName), planSlot(planHeight), planSlot(planInfo), solstorage.Slot(slotLastApplied)} {
		copyString(state, db.GetState, contract, slot)
	}
	for _, name := range []string{"v6.8", "an upgrade name longer than thirty-one bytes"} {
		state.SetState(Address, appliedHeightSlot(name), db.GetState(contract, appliedHeightSlot(name)))
	}

	plan, scheduled, err := CurrentPlan(state)
	require.NoError(t, err)
	require.True(t, scheduled)
	require.Equal(t, Plan{
		Name:   "v7.0",
		Height: 123_456,
		Info:   `{"upgradeType":"minor","binaries":{"linux/amd64":"https://example.com/seid"}}`,
	}, plan)
	require.True(t, plan.IsMinorRelease())
	require.Equal(t, uint64(42), Applied(state, "v6.8"))
	require.Equal(t, uint64(43), Applied(state, "an upgrade name longer than thirty-one bytes"))
	last, err := LastApplied(state)
	require.NoError(t, err)
	require.Equal(t, "an upgrade name longer than thirty-one bytes", last)
}

// copyString copies the word at slot and, for a long string, its data chunks
// from contract into state at Address.
func copyString(state *memState, get func(common.Address, common.Hash) common.Hash, contract common.Address, slot common.Hash) {
	head := get(contract, slot)
	state.SetState(Address, slot, head)
	data := solstorage.ArrayData(slot)
	for i := uint64(0); i < 8; i++ {
		state.SetState(Address, solstorage.Offset(data, i), get(contract, solstorage.Offset(data, i)))
	}
}

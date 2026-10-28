package params

import (
	"encoding/json"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// forgeLayout is `forge inspect ParamsStorage storage-layout --json`.
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

// TestLayoutMatchesForge pins every slot and member offset to the compiler's
// layout of docs/giga-storage/ParamsStorage.sol. Regenerate testdata with
// `forge inspect ParamsStorage storage-layout --json` after editing the schema.
func TestLayoutMatchesForge(t *testing.T) {
	bz, err := os.ReadFile("testdata/params_storage_layout.json")
	require.NoError(t, err)
	var layout forgeLayout
	require.NoError(t, json.Unmarshal(bz, &layout))

	got := map[string]member{}
	for _, e := range layout.Storage {
		got[e.Label] = member{slot: mustUint(t, e.Slot), offset: e.Offset}
	}
	require.Equal(t, map[string]member{"gov": {slotGov, 0}}, got)

	structs := map[string]map[string]member{
		"Gov": {
			"maxDepositPeriod":      govMaxDepositPeriod,
			"votingPeriod":          govVotingPeriod,
			"expeditedVotingPeriod": govExpeditedVotingPeriod,
			"minDeposit":            govMinDeposit,
			"minExpeditedDeposit":   govMinExpeditedDeposit,
			"quorum":                govQuorum,
			"threshold":             govThreshold,
			"vetoThreshold":         govVetoThreshold,
			"expeditedQuorum":       govExpeditedQuorum,
			"expeditedThreshold":    govExpeditedThreshold,
			"__gap":                 {8, 0},
		},
	}
	seen := 0
	for typeID, typ := range layout.Types {
		name, ok := strings.CutPrefix(typeID, "t_struct(")
		if !ok {
			continue
		}
		name = name[:strings.Index(name, ")")]
		want, ok := structs[name]
		require.True(t, ok, "struct %s has no Go layout", name)
		got := map[string]member{}
		for _, e := range typ.Members {
			got[e.Label] = member{slot: mustUint(t, e.Slot), offset: e.Offset}
		}
		require.Equal(t, want, got, name)
		seen++
	}
	require.Equal(t, len(structs), seen)
}

// TestLayoutCoversEveryDeclaredParam checks each declared param sits at a
// member of its subspace's struct.
func TestLayoutCoversEveryDeclaredParam(t *testing.T) {
	members := map[member]bool{
		govMaxDepositPeriod: true, govVotingPeriod: true, govExpeditedVotingPeriod: true,
		govMinDeposit: true, govMinExpeditedDeposit: true, govQuorum: true, govThreshold: true,
		govVetoThreshold: true, govExpeditedQuorum: true, govExpeditedThreshold: true,
	}
	for _, p := range govSubspace.params {
		require.True(t, members[p.at], p.key)
		delete(members, p.at)
	}
	require.Empty(t, members, "every member is declared once")
}

func mustUint(t *testing.T, s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	require.NoError(t, err)
	return v
}

// TestReadsCompilerWrites runs ParamsStorageHarness, which writes
// ParamsStorage with solc's codegen, and reads the result back. Rebuild
// testdata with `forge inspect ParamsStorageHarness deployedBytecode` and the
// remapping giga-storage/=docs/giga-storage/.
func TestReadsCompilerWrites(t *testing.T) {
	code, err := os.ReadFile("testdata/ParamsStorageHarness.bin-runtime")
	require.NoError(t, err)
	seed := common.FromHex("0x7d94792a") // seed()
	_, db, err := runtime.Execute(common.FromHex(strings.TrimSpace(string(code))), seed, &runtime.Config{GasLimit: 10_000_000})
	require.NoError(t, err)
	// runtime.Execute deploys the code at the zero-padded address of "contract".
	values := govSubspace.read(solstorage.Store{State: stateDBView{db}, Address: common.BytesToAddress([]byte("contract"))})
	require.Equal(t, Gov{
		MaxDepositPeriod:      172_800_000_000_000,
		VotingPeriod:          259_200_000_000_000,
		ExpeditedVotingPeriod: 86_400_000_000_001,
		MinDeposit:            big.NewInt(3_500_000_000),
		MinExpeditedDeposit:   big.NewInt(7_000_000_000),
		Quorum:                big.NewInt(334_000_000_000_000_000),
		Threshold:             big.NewInt(500_000_000_000_000_000),
		VetoThreshold:         big.NewInt(334_000_000_000_000_000),
		ExpeditedQuorum:       big.NewInt(667_000_000_000_000_000),
		ExpeditedThreshold:    big.NewInt(667_000_000_000_000_000),
	}, govFrom(values))
}

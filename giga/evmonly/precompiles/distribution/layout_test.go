package distribution

import (
	"encoding/json"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// solcLayout is `solc --storage-layout` of DistributionStorage.
type solcLayout struct {
	Storage []solcEntry `json:"storage"`
	Types   map[string]struct {
		Label         string      `json:"label"`
		NumberOfBytes string      `json:"numberOfBytes"`
		Members       []solcEntry `json:"members"`
	} `json:"types"`
}

type solcEntry struct {
	Label  string `json:"label"`
	Offset int    `json:"offset"`
	Slot   string `json:"slot"`
}

// TestLayoutMatchesSolc pins every slot and member offset to the compiler's
// layout of docs/giga-storage/DistributionStorage.sol. Regenerate testdata with
// `solc --storage-layout` or `forge inspect DistributionStorage storage-layout --json`
// after editing the schema.
func TestLayoutMatchesSolc(t *testing.T) {
	bz, err := os.ReadFile("testdata/distribution_storage_layout.json")
	require.NoError(t, err)
	var layout solcLayout
	require.NoError(t, json.Unmarshal(bz, &layout))

	got := map[string]member{}
	for _, e := range layout.Storage {
		got[e.Label] = member{slot: mustUint(t, e.Slot), offset: e.Offset}
	}
	require.Equal(t, map[string]member{
		"communityPool":                  {slotCommunityPool, 0},
		"previousProposer":               {slotPreviousProposer, 0},
		"validatorOutstandingRewards":    {slotValidatorOutstandingRewards, 0},
		"delegatorWithdrawAddr":          {slotDelegatorWithdrawAddr, 0},
		"delegatorStartingInfo":          {slotDelegatorStartingInfo, 0},
		"validatorHistoricalRewards":     {slotValidatorHistoricalRewards, 0},
		"validatorCurrentRewards":        {slotValidatorCurrentRewards, 0},
		"validatorAccumulatedCommission": {slotValidatorAccumulatedCommission, 0},
		"validatorSlashEvents":           {slotValidatorSlashEvents, 0},
		"epochDAO":                       epochDAO,
		"nextSettlementEpoch":            nextSettlementEpoch,
	}, got)

	structs := map[string]struct {
		members map[string]member
		slots   uint64
	}{
		"struct DistributionStorage.ValidatorHistoricalRewards": {map[string]member{
			"cumulativeRewardRatio": historicalCumulativeRewardRatio,
			"referenceCount":        historicalReferenceCount,
		}, historicalRewardsSlots},
		"struct DistributionStorage.ValidatorCurrentRewards": {map[string]member{
			"rewards": currentRewardsRewards,
			"period":  currentRewardsPeriod,
		}, currentRewardsSlots},
		"struct DistributionStorage.DelegatorStartingInfo": {map[string]member{
			"previousPeriod": startingInfoPreviousPeriod,
			"height":         startingInfoHeight,
			"stake":          startingInfoStake,
		}, startingInfoSlots},
		"struct DistributionStorage.ValidatorSlashEvent": {map[string]member{
			"height":          slashEventHeight,
			"validatorPeriod": slashEventValidatorPeriod,
			"fraction":        slashEventFraction,
			"__gap":           {2, 0},
		}, slashEventSlots},
	}
	seen := 0
	for _, typ := range layout.Types {
		if len(typ.Members) == 0 {
			continue
		}
		want, ok := structs[typ.Label]
		require.True(t, ok, "%s has no Go layout", typ.Label)
		members := map[string]member{}
		for _, e := range typ.Members {
			members[e.Label] = member{slot: mustUint(t, e.Slot), offset: e.Offset}
		}
		require.Equal(t, want.members, members, typ.Label)
		require.Equal(t, want.slots*32, mustUint(t, typ.NumberOfBytes), typ.Label)
		seen++
	}
	require.Equal(t, len(structs), seen)
}

func mustUint(t *testing.T, s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	require.NoError(t, err)
	return v
}

// TestStoreReadsCompilerWrites runs DistributionStorageHarness, which writes
// DistributionStorage with solc's codegen, and reads the result back through
// store. Rebuild testdata from the repository root with solc 0.8.30 and
// `solc --bin-runtime giga-storage/=docs/giga-storage/ giga/evmonly/precompiles/distribution/testdata/DistributionStorageHarness.sol`.
func TestStoreReadsCompilerWrites(t *testing.T) {
	code, err := os.ReadFile("testdata/DistributionStorageHarness.bin-runtime")
	require.NoError(t, err)
	payee := common.HexToAddress("0x00000000000000000000000000000000000000d4")

	seed, err := seedABI.Pack("seed", val1, val2, alice, payee)
	require.NoError(t, err)
	_, db, err := runtime.Execute(common.FromHex(strings.TrimSpace(string(code))), seed, &runtime.Config{GasLimit: 10_000_000})
	require.NoError(t, err)
	// runtime.Execute deploys the code at the zero-padded address of "contract".
	s := store{solstorage.Store{State: stateDBView{db}, Address: common.BytesToAddress([]byte("contract"))}}

	require.Equal(t, big.NewInt(11), s.communityPool())
	require.Equal(t, val2, s.epochDAO())
	require.Equal(t, uint64(81), s.nextSettlementEpoch())
	require.Equal(t, big.NewInt(21), s.outstandingRewards(val1))
	require.Equal(t, payee, s.withdrawAddress(alice))
	require.Equal(t, bob, s.withdrawAddress(bob), "a delegator with no record is paid itself")
	require.Equal(t, startingInfo{previousPeriod: 31, height: 32, stake: ether(33)}, s.startingInfo(val1, alice))
	require.Equal(t, historicalRewards{cumulativeRewardRatio: ether(41), referenceCount: 42}, s.historicalRewards(val1, 31))
	require.Equal(t, currentRewards{rewards: big.NewInt(51), period: 52}, s.currentRewards(val1))
	require.Equal(t, big.NewInt(61), s.accumulatedCommission(val1))

	n, err := s.slashEventCount(val1)
	require.NoError(t, err)
	require.Equal(t, uint64(2), n)
	require.Equal(t, slashEvent{height: 71, validatorPeriod: 72, fraction: big.NewInt(50_000_000_000_000_000)}, s.slashEvent(val1, 0))
	require.Equal(t, slashEvent{height: 73, validatorPeriod: 74, fraction: big.NewInt(500_000_000_000_000_000)}, s.slashEvent(val1, 1))
}

var seedABI = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"seed","inputs":[
		{"name":"val","type":"address"},{"name":"other","type":"address"},
		{"name":"delegator","type":"address"},{"name":"payee","type":"address"}],"outputs":[]}]`))
	if err != nil {
		panic(err)
	}
	return parsed
}()

package staking

import (
	"encoding/json"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// solcLayout is `solc --storage-layout` of StakingStorage.
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
// layout of docs/giga-storage/StakingStorage.sol. Regenerate testdata with
// `solc --storage-layout` or `forge inspect StakingStorage storage-layout --json`
// after editing the schema.
func TestLayoutMatchesSolc(t *testing.T) {
	bz, err := os.ReadFile("testdata/staking_storage_layout.json")
	require.NoError(t, err)
	var layout solcLayout
	require.NoError(t, json.Unmarshal(bz, &layout))

	got := map[string]uint64{}
	for _, e := range layout.Storage {
		require.Zero(t, e.Offset, e.Label)
		got[e.Label] = mustUint(t, e.Slot)
	}
	require.Equal(t, map[string]uint64{
		"lastValidators":              slotLastValidators,
		"lastTotalPower":              slotLastTotalPower,
		"validators":                  slotValidators,
		"validatorSet":                slotValidatorSet,
		"validatorsByConsensusPubkey": slotValidatorsByConsensusPubkey,
		"validatorsByPower":           slotValidatorsByPower,
		"delegations":                 slotDelegations,
		"delegationsByDelegator":      slotDelegationsByDelegator,
		"unbondingDelegations":        slotUnbondingDelegations,
		"redelegationEntryCounts":     slotRedelegationEntryCounts,
		"redelegationsByValDst":       slotRedelegationsByValDst,
		"unbondingQueueBounds":        slotUnbondingQueueBounds,
		"unbondingQueue":              slotUnbondingQueue,
		"redelegationQueueBounds":     slotRedelegationQueueBounds,
		"redelegationQueue":           slotRedelegationQueue,
		"validatorQueueBounds":        slotValidatorQueueBounds,
		"validatorQueue":              slotValidatorQueue,
		"migrating":                   18,
		"migrationCursor":             19,
		"migrationFailures":           20,
	}, got)

	// Each struct's Go members, and how many slots it takes. Description and
	// Commission are read through Validator's flattened members.
	structs := map[string]struct {
		members map[string]member
		slots   uint64
	}{
		"struct StakingStorage.Validator": {map[string]member{
			"consensusPubkey":   validatorConsensusPubkey,
			"jailed":            validatorJailed,
			"status":            validatorStatus,
			"tokens":            validatorTokens,
			"delegatorShares":   validatorDelegatorShares,
			"description":       validatorMoniker,
			"unbondingHeight":   validatorUnbondingHeight,
			"unbondingTime":     validatorUnbondingTime,
			"commission":        validatorCommissionRate,
			"minSelfDelegation": validatorMinSelfDelegation,
			"__gap":             validatorGap,
		}, validatorSlots},
		"struct StakingStorage.Description": {map[string]member{
			"moniker":         sub(validatorMoniker, validatorMoniker),
			"identity":        sub(validatorIdentity, validatorMoniker),
			"website":         sub(validatorWebsite, validatorMoniker),
			"securityContact": sub(validatorSecurityContact, validatorMoniker),
			"details":         sub(validatorDetails, validatorMoniker),
		}, 5},
		"struct StakingStorage.Commission": {map[string]member{
			"commissionRates": sub(validatorCommissionRate, validatorCommissionRate),
			"updateTime":      sub(validatorCommissionUpdate, validatorCommissionRate),
		}, 4},
		"struct StakingStorage.CommissionRates": {map[string]member{
			"rate":          sub(validatorCommissionRate, validatorCommissionRate),
			"maxRate":       sub(validatorCommissionMaxRate, validatorCommissionRate),
			"maxChangeRate": sub(validatorCommissionMaxChange, validatorCommissionRate),
		}, 3},
		"struct StakingStorage.ValidatorPower": {map[string]member{
			"operator": validatorPowerOperator,
			"power":    validatorPowerPower,
		}, 1},
		"struct StakingStorage.Delegation": {map[string]member{"shares": {0, 0}}, 1},
		"struct StakingStorage.UnbondingDelegationEntry": {map[string]member{
			"creationHeight": unbondingEntryCreationHeight,
			"completionTime": unbondingEntryCompletionTime,
			"initialBalance": unbondingEntryInitialBalance,
			"balance":        unbondingEntryBalance,
		}, unbondingEntrySlots},
		"struct StakingStorage.UnbondingQueueEntry": {map[string]member{
			"completionTime": queueCompletionTime,
			"delegator":      unbondingQueueDelegator,
			"validator":      unbondingQueueValidator,
		}, unbondingQueueEntrySlots},
		"struct StakingStorage.RedelegationQueueEntry": {map[string]member{
			"completionTime": queueCompletionTime,
			"delegator":      redelegationQueueDelegator,
			"creationHeight": redelegationQueueCreationHeight,
			"srcValidator":   redelegationQueueSrcValidator,
			"dstValidator":   redelegationQueueDstValidator,
			"initialBalance": redelegationQueueInitialBalance,
			"sharesDst":      redelegationQueueSharesDst,
		}, redelegationQueueEntrySlots},
		"struct StakingStorage.ValidatorQueueEntry": {map[string]member{
			"completionTime":  queueCompletionTime,
			"unbondingHeight": validatorQueueUnbondingHeight,
			"operator":        validatorQueueOperator,
		}, validatorQueueEntrySlots},
		"struct StakingStorage.QueueBounds": {map[string]member{
			"head": queueHead,
			"tail": queueTail,
		}, 1},
		"struct AddressSet": {map[string]member{
			"values":    {0, 0},
			"positions": {1, 0},
		}, 2},
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

// sub returns m's position within the nested struct that starts at base.
func sub(m, base member) member {
	return member{slot: m.slot - base.slot, offset: m.offset}
}

func mustUint(t *testing.T, s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	require.NoError(t, err)
	return v
}

// TestStoreReadsCompilerWrites runs StakingStorageHarness, which writes
// StakingStorage with solc's codegen, and reads the result back through store.
// Rebuild testdata with `solc --bin-runtime` of the harness and the remapping
// giga-storage/=docs/giga-storage/.
func TestStoreReadsCompilerWrites(t *testing.T) {
	code, err := os.ReadFile("testdata/StakingStorageHarness.bin-runtime")
	require.NoError(t, err)
	op := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	other := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	delegator := common.HexToAddress("0x00000000000000000000000000000000000000c3")
	pubkey := common.HexToHash("0xed25519")

	seed, err := seedABI.Pack("seed", op, other, delegator, pubkey)
	require.NoError(t, err)
	_, db, err := runtime.Execute(common.FromHex(strings.TrimSpace(string(code))), seed, &runtime.Config{GasLimit: 10_000_000})
	require.NoError(t, err)
	// runtime.Execute deploys the code at the zero-padded address of "contract".
	s := store{solstorage.Store{State: stateDBView{db}, Address: common.BytesToAddress([]byte("contract"))}}

	set, err := s.lastValidators()
	require.NoError(t, err)
	require.Equal(t, []validatorPower{{op, 30}, {other, 20}}, set)
	require.Equal(t, big.NewInt(50), s.lastTotalPower())

	v, ok := s.validator(op)
	require.True(t, ok)
	require.Equal(t, validator{
		consensusPubkey:   pubkey,
		jailed:            true,
		status:            statusUnbonding,
		tokens:            sei(31),
		delegatorShares:   intToDec(sei(32)),
		unbondingHeight:   33,
		unbondingTime:     34,
		minSelfDelegation: big.NewInt(37),
	}, v)
	base := validatorBase(op)
	moniker, err := s.String(at(base, validatorMoniker))
	require.NoError(t, err)
	require.Equal(t, "a moniker long enough to spill past one storage word", moniker)
	details, err := s.String(at(base, validatorDetails))
	require.NoError(t, err)
	require.Equal(t, "d", details)
	require.Equal(t, big.NewInt(35), s.Uint256(at(base, validatorCommissionRate)))
	require.Equal(t, uint64(36), s.Uint64(at(base, validatorCommissionUpdate), validatorCommissionUpdate.offset))
	require.Equal(t, uint64(2), s.validatorSet().Len())
	require.True(t, s.validatorSet().Contains(solstorage.AddressKey(other)))
	require.Equal(t, op, s.validatorByPubkey(pubkey))
	index := s.powerIndex()
	require.Equal(t, uint64(2), index.len())
	require.Equal(t, other, index.at(0))
	require.Equal(t, op, index.at(1))

	shares, found := s.delegation(delegator, op)
	require.True(t, found)
	require.Equal(t, big.NewInt(41), shares)
	require.True(t, s.delegatedValidators(delegator).Contains(solstorage.AddressKey(op)))
	require.Equal(t, []unbondingEntry{
		{creationHeight: 51, completionTime: 52, initialBalance: big.NewInt(53), balance: big.NewInt(54)},
		{creationHeight: 55, completionTime: 56, initialBalance: big.NewInt(57), balance: big.NewInt(58)},
	}, must(s.unbondingEntries(delegator, op)))
	require.Equal(t, big.NewInt(2), s.redelegationEntryCount(delegator, op, other))
	require.Equal(t, big.NewInt(3), s.receivingRedelegations(other, delegator))

	q := s.unbondingQueue()
	require.Equal(t, uint64(1), q.head())
	require.Equal(t, uint64(2), q.tail())
	e, due := q.due(61)
	require.True(t, due)
	gotDelegator, gotOp := unbondingQueueEntry(s, e)
	require.Equal(t, delegator, gotDelegator)
	require.Equal(t, op, gotOp)
	_, due = q.due(60)
	require.False(t, due)

	e, due = s.redelegationQueue().due(71)
	require.True(t, due)
	require.Equal(t, delegator, s.AddressAt(at(e, redelegationQueueDelegator), redelegationQueueDelegator.offset))
	require.Equal(t, uint64(72), s.Uint64(at(e, redelegationQueueCreationHeight), redelegationQueueCreationHeight.offset))
	require.Equal(t, op, s.AddressAt(at(e, redelegationQueueSrcValidator), redelegationQueueSrcValidator.offset))
	require.Equal(t, other, s.AddressAt(at(e, redelegationQueueDstValidator), redelegationQueueDstValidator.offset))
	require.Equal(t, big.NewInt(73), s.Uint256(at(e, redelegationQueueInitialBalance)))
	require.Equal(t, big.NewInt(74), s.Uint256(at(e, redelegationQueueSharesDst)))

	vq := s.validatorQueue()
	require.Equal(t, uint64(4), vq.head())
	e, due = vq.due(81)
	require.True(t, due)
	require.Equal(t, uint64(82), s.Uint64(at(e, validatorQueueUnbondingHeight), validatorQueueUnbondingHeight.offset))
	require.Equal(t, other, s.AddressAt(at(e, validatorQueueOperator), validatorQueueOperator.offset))
}

var seedABI = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"seed","inputs":[
		{"name":"op","type":"address"},{"name":"other","type":"address"},
		{"name":"delegator","type":"address"},{"name":"pubkey","type":"bytes32"}],"outputs":[]}]`))
	if err != nil {
		panic(err)
	}
	return parsed
}()

// TestDeleteValidatorClearsTheRecord checks that deleting a validator leaves
// none of its slots, including its description's spilled strings.
func TestDeleteValidatorClearsTheRecord(t *testing.T) {
	state := newMemState()
	s := newStore(state)
	op := common.HexToAddress("0xa1")
	base := validatorBase(op)
	s.setValidator(op, validator{consensusPubkey: common.HexToHash("0x01"), status: statusUnbonded, tokens: big.NewInt(1), delegatorShares: big.NewInt(2), unbondingTime: 3, minSelfDelegation: big.NewInt(4)})
	for _, m := range []member{validatorMoniker, validatorIdentity, validatorWebsite, validatorSecurityContact, validatorDetails} {
		require.NoError(t, s.SetString(at(base, m), strings.Repeat("x", 100)))
	}
	s.SetUint256(at(base, validatorCommissionMaxChange), big.NewInt(5))
	s.SetUint256(solstorage.Offset(base, validatorSlots-1), big.NewInt(6))
	require.NoError(t, s.deleteValidator(op))
	require.Empty(t, state.storage[Address])
}

// stateDBView reads storage from a geth StateDB. Only GetState is supported.
type stateDBView struct {
	db vm.StateDB
}

func (v stateDBView) GetState(a common.Address, k common.Hash) common.Hash {
	return v.db.GetState(a, k)
}

func (v stateDBView) SetState(common.Address, common.Hash, common.Hash) { panic("read-only") }
func (v stateDBView) GetBalance(common.Address) *big.Int                { panic("unsupported") }
func (v stateDBView) AddBalance(common.Address, *big.Int)               { panic("unsupported") }
func (v stateDBView) SubBalance(common.Address, *big.Int) error         { panic("unsupported") }
func (v stateDBView) GetNonce(common.Address) uint64                    { panic("unsupported") }
func (v stateDBView) SetNonce(common.Address, uint64)                   { panic("unsupported") }
func (v stateDBView) GetCode(common.Address) []byte                     { panic("unsupported") }
func (v stateDBView) Failed() bool                                      { return false }
func (v stateDBView) Snapshot() int                                     { panic("unsupported") }
func (v stateDBView) RevertToSnapshot(int)                              { panic("unsupported") }

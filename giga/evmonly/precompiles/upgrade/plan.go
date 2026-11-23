package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// Limits on a plan's fields.
const (
	MaxNameLength = 128
	MaxInfoLength = 4 << 10
)

// Plan is a scheduled software upgrade. Height is an EVM block number.
type Plan struct {
	Name   string
	Height uint64
	Info   string
}

var (
	errEmptyName       = errors.New("upgrade name is empty")
	errNameTooLong     = fmt.Errorf("upgrade name is longer than %d bytes", MaxNameLength)
	errInfoTooLong     = fmt.Errorf("upgrade info is longer than %d bytes", MaxInfoLength)
	errZeroHeight      = errors.New("upgrade height must be greater than 0")
	errHeightTooLarge  = fmt.Errorf("upgrade height is greater than %d", int64(math.MaxInt64))
	errGenesisWritten  = errors.New("applied upgrades are already written")
	errRepeatedGenesis = errors.New("applied upgrade names repeat")
	errNotScheduled    = errors.New("upgrade is not scheduled")
	errNameCharacter   = errors.New("upgrade name may hold only ASCII letters, digits, '.', '+' and '-'")
)

// ValidateName returns an error if name is empty, longer than MaxNameLength,
// or holds a character other than an ASCII letter, a digit, '.', '+' or '-'.
func ValidateName(name string) error {
	switch {
	case name == "":
		return errEmptyName
	case len(name) > MaxNameLength:
		return errNameTooLong
	case strings.IndexFunc(name, notNameCharacter) >= 0:
		return errNameCharacter
	}
	return nil
}

// notNameCharacter reports whether r is outside the characters of a semver string.
func notNameCharacter(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', '0' <= r && r <= '9', r == '.', r == '+', r == '-':
		return false
	}
	return true
}

// ValidateBasic checks p's fields without reading state.
func (p Plan) ValidateBasic() error {
	if err := ValidateName(p.Name); err != nil {
		return err
	}
	switch {
	case len(p.Info) > MaxInfoLength:
		return errInfoTooLong
	case p.Height == 0:
		return errZeroHeight
	case p.Height > math.MaxInt64:
		return errHeightTooLarge
	}
	return nil
}

// IsMinorRelease reports whether p's Info marks it a minor release: a JSON
// object whose upgradeType is "minor", in any case. Any other Info, including
// one that is not JSON, is a major release.
func (p Plan) IsMinorRelease() bool {
	var details struct {
		UpgradeType string `json:"upgradeType"`
	}
	if p.Info == "" || json.Unmarshal([]byte(p.Info), &details) != nil {
		return false
	}
	return strings.EqualFold(details.UpgradeType, "minor")
}

// Check returns an error if p cannot be scheduled at block: it fails
// ValidateBasic, its height is below block, or its name was already applied.
func Check(state precompiles.State, block uint64, p Plan) error {
	if err := p.ValidateBasic(); err != nil {
		return err
	}
	if p.Height < block {
		return fmt.Errorf("upgrade height %d is below the current block %d", p.Height, block)
	}
	if height := Applied(state, p.Name); height != 0 {
		return fmt.Errorf("upgrade %q was already applied at block %d", p.Name, height)
	}
	return nil
}

// Schedule replaces the scheduled plan with p if Check allows it at block. It
// writes nothing when it returns an error.
func Schedule(state precompiles.State, block uint64, p Plan) error {
	if err := Check(state, block, p); err != nil {
		return err
	}
	return writePlan(newStore(state), p)
}

// CheckCancel returns an error unless the scheduled plan is named name.
func CheckCancel(state precompiles.State, name string) error {
	plan, scheduled, err := scheduledPlan(state)
	switch {
	case err != nil:
		return err
	case !scheduled:
		return fmt.Errorf("%w: %q, and no plan is scheduled", errNotScheduled, name)
	case plan.Name != name:
		return fmt.Errorf("%w: %q, the scheduled plan is %q", errNotScheduled, name, plan.Name)
	}
	return nil
}

// Cancel clears the scheduled plan if CheckCancel allows it. It writes nothing
// when it returns an error.
func Cancel(state precompiles.State, name string) error {
	if err := CheckCancel(state, name); err != nil {
		return err
	}
	return writePlan(newStore(state), Plan{})
}

// CurrentPlan returns the scheduled plan, and false when none is scheduled.
func CurrentPlan(state precompiles.State) (Plan, bool, error) {
	plan, scheduled, err := scheduledPlan(state)
	if err != nil || !scheduled {
		return Plan{}, false, err
	}
	if plan.Info, err = planInfoOf(state); err != nil {
		return Plan{}, false, err
	}
	return plan, true, nil
}

// scheduledPlan returns the scheduled plan's name and height, leaving Info
// empty, and false when none is scheduled.
func scheduledPlan(state precompiles.State) (Plan, bool, error) {
	s := newStore(state)
	height := s.Uint64(planSlot(planHeight), 0)
	if height == 0 {
		return Plan{}, false, nil
	}
	name, err := s.String(planSlot(planName))
	if err != nil {
		return Plan{}, false, fmt.Errorf("upgrade plan name: %w", err)
	}
	return Plan{Name: name, Height: height}, true, nil
}

// planInfoOf returns the scheduled plan's Info.
func planInfoOf(state precompiles.State) (string, error) {
	info, err := newStore(state).String(planSlot(planInfo))
	if err != nil {
		return "", fmt.Errorf("upgrade plan info: %w", err)
	}
	return info, nil
}

// Applied returns the block the named upgrade was applied at, or 0 when it
// has not been applied.
func Applied(state precompiles.State, name string) uint64 {
	return newStore(state).Uint64(appliedHeightSlot(name), 0)
}

// LastApplied returns the name of the upgrade applied last, or "" when none
// has been.
func LastApplied(state precompiles.State) (string, error) {
	return newStore(state).String(solstorage.Slot(slotLastApplied))
}

// HasGenesis reports whether WriteGenesis has recorded any applied upgrade.
func HasGenesis(state precompiles.State) bool {
	return newStore(state).Word(solstorage.Slot(slotLastApplied)) != (common.Hash{})
}

// CheckGenesisNames returns an error if a name is empty, longer than
// MaxNameLength, or repeated.
func CheckGenesisNames(names []string) error {
	for i, name := range names {
		switch {
		case name == "":
			return fmt.Errorf("applied upgrade %d: %w", i, errEmptyName)
		case len(name) > MaxNameLength:
			return fmt.Errorf("applied upgrade %d: %w", i, errNameTooLong)
		case slices.Contains(names[:i], name):
			return fmt.Errorf("%w: %q", errRepeatedGenesis, name)
		}
	}
	return nil
}

// WriteGenesis records names, in order, as applied at height; the last becomes
// LastApplied. An empty names writes nothing. It returns an error, writing
// nothing, when CheckGenesisNames rejects names or applied upgrades are already
// recorded.
func WriteGenesis(state precompiles.State, names []string, height uint64) error {
	if HasGenesis(state) {
		return errGenesisWritten
	}
	if err := CheckGenesisNames(names); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	s := newStore(state)
	for _, name := range names {
		s.SetUint64(appliedHeightSlot(name), 0, height)
	}
	return setString(s, solstorage.Slot(slotLastApplied), names[len(names)-1])
}

// markApplied records name as applied at height and as LastApplied, and
// clears the plan.
func markApplied(s solstorage.Store, name string, height uint64) error {
	s.SetUint64(appliedHeightSlot(name), 0, height)
	if err := setString(s, solstorage.Slot(slotLastApplied), name); err != nil {
		return err
	}
	return writePlan(s, Plan{})
}

// writePlan stores p as the plan; a zero p clears it.
func writePlan(s solstorage.Store, p Plan) error {
	if err := setString(s, planSlot(planName), p.Name); err != nil {
		return err
	}
	s.SetUint64(planSlot(planHeight), 0, p.Height)
	return setString(s, planSlot(planInfo), p.Info)
}

// setString stores v at slot, over any previous value. It fails only when v is
// longer than solstorage.MaxStringLength.
func setString(s solstorage.Store, slot common.Hash, v string) error {
	// Clearing the head word drops a previous value that does not decode.
	if _, err := s.StringChunks(slot); err != nil {
		s.SetWord(slot, common.Hash{})
	}
	return s.SetString(slot, v)
}

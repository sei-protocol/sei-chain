package staking

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// Validator statuses, numbered as x/staking's BondStatus.
const (
	statusUnbonded  uint8 = 1
	statusUnbonding uint8 = 2
	statusBonded    uint8 = 3
)

var (
	errInvalidStoredSet     = errors.New("invalid stored bonded set")
	errQueueCorrupt         = errors.New("queue head is past its tail")
	errQueueExhausted       = errors.New("queue has used every index")
	errPowerIndexCorrupt    = errors.New("validator is not where the power index orders it")
	errInsufficientShares   = errors.New("insufficient delegation shares")
	errInvalidStoredEntries = errors.New("invalid stored unbonding entries")
)

// validator is the StakingStorage.Validator fields staking's rules read and
// write. Its description and commission are left to the methods that set them.
type validator struct {
	consensusPubkey   common.Hash
	jailed            bool
	status            uint8
	tokens            *big.Int
	delegatorShares   *big.Int
	unbondingHeight   uint64
	unbondingTime     uint64
	minSelfDelegation *big.Int
}

func (v validator) bonded() bool {
	return v.status == statusBonded
}

// invalidExRate reports whether v has shares but no tokens to price them.
func (v validator) invalidExRate() bool {
	return v.tokens.Sign() == 0 && v.delegatorShares.Sign() > 0
}

// sharesFromTokens returns the shares amount is worth at v's exchange rate,
// truncated, as x/staking's SharesFromTokens does. It fails when v has no
// tokens.
func (v validator) sharesFromTokens(amount *big.Int) (*big.Int, error) {
	if v.tokens.Sign() == 0 {
		return nil, errInsufficientShares
	}
	shares := new(big.Int).Mul(v.delegatorShares, amount)
	return shares.Quo(shares, v.tokens), nil
}

// tokensFromShares returns the decimal tokens shares are worth at v's exchange
// rate, as x/staking's TokensFromShares does, or zero when v has no shares.
func (v validator) tokensFromShares(shares *big.Int) *big.Int {
	if v.delegatorShares.Sign() == 0 {
		return new(big.Int)
	}
	return dec.Quo(new(big.Int).Mul(shares, v.tokens), v.delegatorShares)
}

// store is StakingStorage at the staking precompile's address.
type store struct {
	solstorage.Store
}

func newStore(state precompiles.State) store {
	return store{solstorage.Store{State: state, Address: Address}}
}

func at(base common.Hash, m member) common.Hash {
	return solstorage.Offset(base, m.slot)
}

// params returns staking's params from the params precompile.
func (s store) params() chainparams.Staking {
	return chainparams.ReadStaking(s.State)
}

func validatorBase(op common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.Slot(slotValidators), op)
}

// validator returns operator op's validator, reporting whether it exists.
func (s store) validator(op common.Address) (validator, bool) {
	base := validatorBase(op)
	status := s.Uint8(at(base, validatorStatus), validatorStatus.offset)
	if status == 0 {
		return validator{}, false
	}
	return validator{
		consensusPubkey:   s.Word(at(base, validatorConsensusPubkey)),
		jailed:            s.Bool(at(base, validatorJailed), validatorJailed.offset),
		status:            status,
		tokens:            s.Uint256(at(base, validatorTokens)),
		delegatorShares:   s.Uint256(at(base, validatorDelegatorShares)),
		unbondingHeight:   s.Uint64(at(base, validatorUnbondingHeight), validatorUnbondingHeight.offset),
		unbondingTime:     s.Uint64(at(base, validatorUnbondingTime), validatorUnbondingTime.offset),
		minSelfDelegation: s.Uint256(at(base, validatorMinSelfDelegation)),
	}, true
}

func (s store) setValidator(op common.Address, v validator) {
	base := validatorBase(op)
	s.SetWord(at(base, validatorConsensusPubkey), v.consensusPubkey)
	s.SetBool(at(base, validatorJailed), validatorJailed.offset, v.jailed)
	s.SetUint8(at(base, validatorStatus), validatorStatus.offset, v.status)
	s.SetUint256(at(base, validatorTokens), v.tokens)
	s.SetUint256(at(base, validatorDelegatorShares), v.delegatorShares)
	s.SetUint64(at(base, validatorUnbondingHeight), validatorUnbondingHeight.offset, v.unbondingHeight)
	s.SetUint64(at(base, validatorUnbondingTime), validatorUnbondingTime.offset, v.unbondingTime)
	s.SetUint256(at(base, validatorMinSelfDelegation), v.minSelfDelegation)
}

// deleteValidator zeroes every slot of op's validator, including its
// description's spilled strings.
func (s store) deleteValidator(op common.Address) error {
	base := validatorBase(op)
	for _, m := range []member{validatorMoniker, validatorIdentity, validatorWebsite, validatorSecurityContact, validatorDetails} {
		if err := s.SetString(at(base, m), ""); err != nil {
			return err
		}
	}
	for i := uint64(0); i < validatorSlots; i++ {
		s.SetWord(solstorage.Offset(base, i), common.Hash{})
	}
	return nil
}

// validatorPower returns the consensus power of op's stored tokens.
func (s store) validatorPower(op common.Address) *big.Int {
	return consensusPower(s.Uint256(at(validatorBase(op), validatorTokens)))
}

func (s store) validatorSet() solstorage.Set {
	return solstorage.Set{Store: s.Store, Base: solstorage.Slot(slotValidatorSet)}
}

func validatorByPubkeySlot(pubkey common.Hash) common.Hash {
	return solstorage.MapWord(solstorage.Slot(slotValidatorsByConsensusPubkey), pubkey)
}

// validatorByPubkey returns the operator whose validator has consensus key
// pubkey, or the zero address.
func (s store) validatorByPubkey(pubkey common.Hash) common.Address {
	return s.AddressAt(validatorByPubkeySlot(pubkey), 0)
}

func (s store) setValidatorByPubkey(pubkey common.Hash, op common.Address) {
	s.SetAddressAt(validatorByPubkeySlot(pubkey), 0, op)
}

// validatorPower is StakingStorage.ValidatorPower.
type validatorPower struct {
	operator common.Address
	power    uint64
}

// lastValidators returns the bonded set as last stored, failing on a set longer
// than any MaxValidators allows.
func (s store) lastValidators() ([]validatorPower, error) {
	base := solstorage.Slot(slotLastValidators)
	n := s.Uint64(base, 0)
	if n > uint64(chainparams.MaxBondedValidators) {
		return nil, fmt.Errorf("%w: %d validators", errInvalidStoredSet, n)
	}
	data := solstorage.ArrayData(base)
	out := make([]validatorPower, n)
	for i := range out {
		slot := solstorage.Offset(data, uint64(i)) //nolint:gosec // i is below n
		out[i] = validatorPower{
			operator: s.AddressAt(slot, validatorPowerOperator.offset),
			power:    s.Uint64(slot, validatorPowerPower.offset),
		}
	}
	return out, nil
}

// setLastValidators replaces the bonded set, zeroing elements the new set no
// longer covers.
func (s store) setLastValidators(set []validatorPower) {
	base := solstorage.Slot(slotLastValidators)
	old := min(s.Uint64(base, 0), uint64(chainparams.MaxBondedValidators))
	data := solstorage.ArrayData(base)
	for i, v := range set {
		slot := solstorage.Offset(data, uint64(i)) //nolint:gosec // i is non-negative
		s.SetWord(slot, common.Hash{})
		s.SetAddressAt(slot, validatorPowerOperator.offset, v.operator)
		s.SetUint64(slot, validatorPowerPower.offset, v.power)
	}
	for i := uint64(len(set)); i < old; i++ {
		s.SetWord(solstorage.Offset(data, i), common.Hash{})
	}
	s.SetWord(base, solstorage.Slot(uint64(len(set))))
}

// lastTotalPower returns the consensus power of the bonded set as last stored.
func (s store) lastTotalPower() *big.Int {
	return s.Uint256(solstorage.Slot(slotLastTotalPower))
}

func (s store) setLastTotalPower(power *big.Int) {
	s.SetUint256(solstorage.Slot(slotLastTotalPower), power)
}

func delegationSlot(delegator, op common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.MapAddress(solstorage.Slot(slotDelegations), delegator), op)
}

// delegatedValidators is the validators delegator delegates to.
func (s store) delegatedValidators(delegator common.Address) solstorage.Set {
	return solstorage.Set{Store: s.Store, Base: solstorage.MapAddress(solstorage.Slot(slotDelegationsByDelegator), delegator)}
}

// delegation returns delegator's shares in op and whether delegator delegates
// to op; a delegation can exist with no shares, as x/staking's can.
func (s store) delegation(delegator, op common.Address) (*big.Int, bool) {
	shares := s.Uint256(delegationSlot(delegator, op))
	if shares.Sign() > 0 {
		return shares, true
	}
	return shares, s.delegatedValidators(delegator).Contains(solstorage.AddressKey(op))
}

func (s store) setDelegation(delegator, op common.Address, shares *big.Int) {
	s.SetUint256(delegationSlot(delegator, op), shares)
	s.delegatedValidators(delegator).Add(solstorage.AddressKey(op))
}

func (s store) removeDelegation(delegator, op common.Address) {
	s.SetUint256(delegationSlot(delegator, op), nil)
	s.delegatedValidators(delegator).Remove(solstorage.AddressKey(op))
}

// unbondingEntry is StakingStorage.UnbondingDelegationEntry.
type unbondingEntry struct {
	creationHeight uint64
	completionTime uint64
	initialBalance *big.Int
	balance        *big.Int
}

func unbondingBase(delegator, op common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.MapAddress(solstorage.Slot(slotUnbondingDelegations), delegator), op)
}

// unbondingEntryCount returns how many entries delegator's unbonding from op
// holds.
func (s store) unbondingEntryCount(delegator, op common.Address) uint64 {
	return s.Uint64(unbondingBase(delegator, op), 0)
}

// unbondingEntries returns delegator's unbonding entries from op, oldest first,
// failing on more entries than any MaxEntries allows.
func (s store) unbondingEntries(delegator, op common.Address) ([]unbondingEntry, error) {
	base := unbondingBase(delegator, op)
	data := solstorage.ArrayData(base)
	n := s.Uint64(base, 0)
	if n > math.MaxUint32 {
		return nil, fmt.Errorf("%w: %d entries", errInvalidStoredEntries, n)
	}
	var out []unbondingEntry
	for i := uint64(0); i < n && !s.State.Failed(); i++ {
		e := solstorage.Offset(data, i*unbondingEntrySlots)
		out = append(out, unbondingEntry{
			creationHeight: s.Uint64(at(e, unbondingEntryCreationHeight), unbondingEntryCreationHeight.offset),
			completionTime: s.Uint64(at(e, unbondingEntryCompletionTime), unbondingEntryCompletionTime.offset),
			initialBalance: s.Uint256(at(e, unbondingEntryInitialBalance)),
			balance:        s.Uint256(at(e, unbondingEntryBalance)),
		})
	}
	return out, nil
}

// setUnbondingEntries replaces delegator's unbonding entries from op, zeroing
// the elements of the old entries that the new ones no longer cover.
func (s store) setUnbondingEntries(delegator, op common.Address, entries []unbondingEntry) {
	base := unbondingBase(delegator, op)
	data := solstorage.ArrayData(base)
	old := s.Uint64(base, 0)
	for i, entry := range entries {
		s.setUnbondingEntry(solstorage.Offset(data, uint64(i)*unbondingEntrySlots), entry) //nolint:gosec // i is non-negative
	}
	for i := uint64(len(entries)) * unbondingEntrySlots; i < old*unbondingEntrySlots && !s.State.Failed(); i++ {
		s.SetWord(solstorage.Offset(data, i), common.Hash{})
	}
	s.SetWord(base, solstorage.Slot(uint64(len(entries))))
}

// appendUnbondingEntry adds entry after delegator's other unbonding entries
// from op.
func (s store) appendUnbondingEntry(delegator, op common.Address, entry unbondingEntry) {
	base := unbondingBase(delegator, op)
	n := s.Uint64(base, 0)
	s.setUnbondingEntry(solstorage.Offset(solstorage.ArrayData(base), n*unbondingEntrySlots), entry)
	s.SetWord(base, solstorage.Slot(n+1))
}

func (s store) setUnbondingEntry(e common.Hash, entry unbondingEntry) {
	s.SetWord(at(e, unbondingEntryCreationHeight), common.Hash{})
	s.SetUint64(at(e, unbondingEntryCreationHeight), unbondingEntryCreationHeight.offset, entry.creationHeight)
	s.SetUint64(at(e, unbondingEntryCompletionTime), unbondingEntryCompletionTime.offset, entry.completionTime)
	s.SetUint256(at(e, unbondingEntryInitialBalance), entry.initialBalance)
	s.SetUint256(at(e, unbondingEntryBalance), entry.balance)
}

func redelegationCountSlot(delegator, src, dst common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.MapAddress(solstorage.MapAddress(solstorage.Slot(slotRedelegationEntryCounts), delegator), src), dst)
}

// redelegationEntryCount returns how many of delegator's redelegations from
// src to dst have not completed.
func (s store) redelegationEntryCount(delegator, src, dst common.Address) *big.Int {
	return s.Uint256(redelegationCountSlot(delegator, src, dst))
}

func (s store) setRedelegationEntryCount(delegator, src, dst common.Address, n *big.Int) {
	s.SetUint256(redelegationCountSlot(delegator, src, dst), n)
}

func receivingSlot(dst, delegator common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.MapAddress(solstorage.Slot(slotRedelegationsByValDst), dst), delegator)
}

// receivingRedelegations returns how many of delegator's redelegations into dst
// have not completed.
func (s store) receivingRedelegations(dst, delegator common.Address) *big.Int {
	return s.Uint256(receivingSlot(dst, delegator))
}

func (s store) setReceivingRedelegations(dst, delegator common.Address, n *big.Int) {
	s.SetUint256(receivingSlot(dst, delegator), n)
}

// queue is one of StakingStorage's first-in first-out queues: the entries at
// indexes [head, tail) of a mapping, each width slots and each beginning with
// its completion time, which never decreases along the queue.
type queue struct {
	s       store
	bounds  common.Hash
	entries common.Hash
	width   uint64
}

func (s store) unbondingQueue() queue {
	return queue{s, solstorage.Slot(slotUnbondingQueueBounds), solstorage.Slot(slotUnbondingQueue), unbondingQueueEntrySlots}
}

// unbondingQueueEntry returns the delegator and validator unbonding queue entry
// e names.
func unbondingQueueEntry(s store, e common.Hash) (delegator, op common.Address) {
	return s.AddressAt(at(e, unbondingQueueDelegator), unbondingQueueDelegator.offset),
		s.AddressAt(at(e, unbondingQueueValidator), unbondingQueueValidator.offset)
}

func (s store) redelegationQueue() queue {
	return queue{s, solstorage.Slot(slotRedelegationQueueBounds), solstorage.Slot(slotRedelegationQueue), redelegationQueueEntrySlots}
}

func (s store) validatorQueue() queue {
	return queue{s, solstorage.Slot(slotValidatorQueueBounds), solstorage.Slot(slotValidatorQueue), validatorQueueEntrySlots}
}

func (q queue) head() uint64 {
	return q.s.Uint64(at(q.bounds, queueHead), queueHead.offset)
}

func (q queue) tail() uint64 {
	return q.s.Uint64(at(q.bounds, queueTail), queueTail.offset)
}

// check fails when the queue's head is past its tail.
func (q queue) check() error {
	if head, tail := q.head(), q.tail(); head > tail {
		return fmt.Errorf("%w: head %d, tail %d", errQueueCorrupt, head, tail)
	}
	return nil
}

func (q queue) entry(i uint64) common.Hash {
	return solstorage.MapUint64(q.entries, i)
}

// due returns the head entry's base slot if it completes at or before now.
func (q queue) due(now uint64) (common.Hash, bool) {
	head := q.head()
	if head >= q.tail() {
		return common.Hash{}, false
	}
	e := q.entry(head)
	if q.s.Uint64(at(e, queueCompletionTime), queueCompletionTime.offset) > now {
		return common.Hash{}, false
	}
	return e, true
}

// clamp returns t, or the last entry's completion time when that is later, so
// an entry pushed at the result keeps the queue in completion order.
func (q queue) clamp(t uint64) uint64 {
	head, tail := q.head(), q.tail()
	if tail <= head {
		return t
	}
	return max(t, q.s.Uint64(at(q.entry(tail-1), queueCompletionTime), queueCompletionTime.offset))
}

// push appends an entry completing at t, which clamp must have returned, and
// returns its base slot for the caller to write the rest of.
func (q queue) push(t uint64) (common.Hash, error) {
	tail := q.tail()
	if tail == math.MaxUint64 {
		return common.Hash{}, errQueueExhausted
	}
	e := q.entry(tail)
	q.s.SetUint64(at(e, queueCompletionTime), queueCompletionTime.offset, t)
	q.s.SetUint64(at(q.bounds, queueTail), queueTail.offset, tail+1)
	return e, nil
}

// pop zeroes the head entry and moves the head past it.
func (q queue) pop() {
	head := q.head()
	e := q.entry(head)
	for i := uint64(0); i < q.width; i++ {
		q.s.SetWord(solstorage.Offset(e, i), common.Hash{})
	}
	q.s.SetUint64(at(q.bounds, queueHead), queueHead.offset, head+1)
}

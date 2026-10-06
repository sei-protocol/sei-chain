package types

import (
	"encoding/binary"
	"fmt"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/address"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"
)

const (
	// ModuleName is the name of the module
	ModuleName = "gov"

	// StoreKey is the store key string for gov
	StoreKey = ModuleName

	// RouterKey is the message route for gov
	RouterKey = ModuleName

	// QuerierRoute is the querier route for gov
	QuerierRoute = ModuleName
)

// Keys for governance store
// Items are stored with the following key: values
//
// - 0x00<proposalID_Bytes>: Proposal
//
// - 0x01<endTime_Bytes><proposalID_Bytes>: activeProposalID
//
// - 0x02<endTime_Bytes><proposalID_Bytes>: inactiveProposalID
//
// - 0x03: nextProposalID
//
// - 0x10<proposalID_Bytes><depositorAddrLen (1 Byte)><depositorAddr_Bytes>: Deposit
//
// - 0x20<proposalID_Bytes><voterAddrLen (1 Byte)><voterAddr_Bytes>: Voter
//
// - 0x30<proposalID_Bytes>: Tally progress
//
// - 0x31<proposalID_Bytes><tallyRound_Byte><voterAddrLen (1 Byte)><voterAddr_Bytes>: Archived voter
//
// - 0x32<proposalID_Bytes><tallyRound_Byte>: Tally archive cleanup cursor
//
// - 0x33<proposalID_Bytes><voterAddrLen (1 Byte)><voterAddr_Bytes>: Voter delegation snapshot
//
// - 0x34<proposalID_Bytes><tallyRound_Byte><voterAddrLen (1 Byte)><voterAddr_Bytes>: Archived voter delegation snapshot
//
// - 0x35<voterAddrLen (1 Byte)><voterAddr_Bytes><proposalID_Bytes>: Active proposal voted on by address
//
// - 0x36: First proposal ID that does not require delegation-tracking backfill
//
// - 0x37<proposalID_Bytes>: Delegation-tracking backfill cursor
//
// - 0x38: Latest deferred vote-delegation update sequence
//
// - 0x39<sequence_Bytes>: Deferred vote-delegation update
//
// - 0x3A<voterAddrLen (1 Byte)><voterAddr_Bytes><sequence_Bytes>: Deferred update index by voter
//
// - 0x3B<proposalID_Bytes><voterAddrLen (1 Byte)><voterAddr_Bytes>: Last applied delegation update sequence
//
// - 0x3C<endTime_Bytes><proposalID_Bytes>: Proposal deadline awaiting tally completion
//
// - 0x3D: Last block time checked for proposal deadlines
//
// - 0x3E<boundaryID_Bytes>: Frozen tally electorate
//
// - 0x3F<upperTime_Bytes>: Frozen electorate for deadlines between block times
//
// - 0x40<endTime_Bytes>: Frozen electorate for deadlines equal to a block time
//
// - 0x41<proposalID_Bytes>: Frozen electorate selected for a proposal tally round
//
// - 0x42: Incremental tally activation marker

// - 0x43<proposalID_Bytes>: Legacy proposal's post-expedited modern tally round

// - 0x44<proposalID_Bytes><tallyRound_Byte><validatorAddrLen (1 Byte)><validatorAddr_Bytes>: Tally validator accumulator

// - 0x45<proposalID_Bytes><tallyRound_Byte>: Tally validator accumulator cleanup cursor
var (
	ProposalsKeyPrefix          = []byte{0x00}
	ActiveProposalQueuePrefix   = []byte{0x01}
	InactiveProposalQueuePrefix = []byte{0x02}
	ProposalIDKey               = []byte{0x03}

	DepositsKeyPrefix = []byte{0x10}

	VotesKeyPrefix = []byte{0x20}

	TallyProgressKeyPrefix        = []byte{0x30}
	TallyVotesKeyPrefix           = []byte{0x31}
	TallyCleanupKeyPrefix         = []byte{0x32}
	VoteDelegationsKeyPrefix      = []byte{0x33}
	TallyVoteDelegationsKeyPrefix = []byte{0x34}
	VoterProposalsKeyPrefix       = []byte{0x35}

	VoteDelegationBackfillCutoffKey         = []byte{0x36}
	VoteDelegationBackfillProgressKeyPrefix = []byte{0x37}

	VoteDelegationUpdateSequenceKey         = []byte{0x38}
	VoteDelegationUpdatesKeyPrefix          = []byte{0x39}
	VoterVoteDelegationUpdatesKeyPrefix     = []byte{0x3A}
	VoteDelegationSnapshotRevisionKeyPrefix = []byte{0x3B}

	ProposalDeadlineKeyPrefix           = []byte{0x3C}
	DeadlineBoundaryBlockTimeKey        = []byte{0x3D}
	TallyBoundaryMetaKeyPrefix          = []byte{0x3E}
	GapTallyBoundaryKeyPrefix           = []byte{0x3F}
	ExactTallyBoundaryKeyPrefix         = []byte{0x40}
	ProposalTallyBoundaryKeyPrefix      = []byte{0x41}
	IncrementalTallyEnabledKey          = []byte{0x42}
	ModernTallyRoundKeyPrefix           = []byte{0x43}
	TallyValidatorAccumulatorsKeyPrefix = []byte{0x44}
	TallyAccumulatorCleanupKeyPrefix    = []byte{0x45}
)

var lenTime = len(sdk.FormatTimeBytes(time.Now()))

// GetProposalIDBytes returns the byte representation of the proposalID
func GetProposalIDBytes(proposalID uint64) (proposalIDBz []byte) {
	proposalIDBz = make([]byte, 8)
	binary.BigEndian.PutUint64(proposalIDBz, proposalID)
	return
}

// GetProposalIDFromBytes returns proposalID in uint64 format from a byte array
func GetProposalIDFromBytes(bz []byte) (proposalID uint64) {
	return binary.BigEndian.Uint64(bz)
}

// ProposalKey gets a specific proposal from the store
func ProposalKey(proposalID uint64) []byte {
	return append(ProposalsKeyPrefix, GetProposalIDBytes(proposalID)...)
}

// ActiveProposalByTimeKey gets the active proposal queue key by endTime
func ActiveProposalByTimeKey(endTime time.Time) []byte {
	return append(ActiveProposalQueuePrefix, sdk.FormatTimeBytes(endTime)...)
}

// ActiveProposalQueueKey returns the key for a proposalID in the activeProposalQueue
func ActiveProposalQueueKey(proposalID uint64, endTime time.Time) []byte {
	return append(ActiveProposalByTimeKey(endTime), GetProposalIDBytes(proposalID)...)
}

// InactiveProposalByTimeKey gets the inactive proposal queue key by endTime
func InactiveProposalByTimeKey(endTime time.Time) []byte {
	return append(InactiveProposalQueuePrefix, sdk.FormatTimeBytes(endTime)...)
}

// InactiveProposalQueueKey returns the key for a proposalID in the inactiveProposalQueue
func InactiveProposalQueueKey(proposalID uint64, endTime time.Time) []byte {
	return append(InactiveProposalByTimeKey(endTime), GetProposalIDBytes(proposalID)...)
}

// DepositsKey gets the first part of the deposits key based on the proposalID
func DepositsKey(proposalID uint64) []byte {
	return append(DepositsKeyPrefix, GetProposalIDBytes(proposalID)...)
}

// DepositKey key of a specific deposit from the store
func DepositKey(proposalID uint64, depositorAddr sdk.AccAddress) []byte {
	return append(DepositsKey(proposalID), address.MustLengthPrefix(depositorAddr.Bytes())...)
}

// VotesKey gets the first part of the votes key based on the proposalID
func VotesKey(proposalID uint64) []byte {
	return append(VotesKeyPrefix, GetProposalIDBytes(proposalID)...)
}

// VoteKey key of a specific vote from the store
func VoteKey(proposalID uint64, voterAddr sdk.AccAddress) []byte {
	return append(VotesKey(proposalID), address.MustLengthPrefix(voterAddr.Bytes())...)
}

// VoteDelegationsKey returns the key for a vote's current delegation snapshot.
func VoteDelegationsKey(proposalID uint64, voterAddr sdk.AccAddress) []byte {
	return append(append(VoteDelegationsKeyPrefix, GetProposalIDBytes(proposalID)...), address.MustLengthPrefix(voterAddr.Bytes())...)
}

// VoterProposalsKey returns the key indexing an address's vote on an active proposal.
func VoterProposalsKey(voterAddr sdk.AccAddress, proposalID uint64) []byte {
	return append(VoterProposalsKeyPrefixForAddress(voterAddr), GetProposalIDBytes(proposalID)...)
}

// VoterProposalsKeyPrefixForAddress returns the active-proposal vote prefix for an address.
func VoterProposalsKeyPrefixForAddress(voterAddr sdk.AccAddress) []byte {
	return append(VoterProposalsKeyPrefix, address.MustLengthPrefix(voterAddr.Bytes())...)
}

// TallyProgressKey returns the key for a proposal's incremental tally state.
func TallyProgressKey(proposalID uint64) []byte {
	return append(TallyProgressKeyPrefix, GetProposalIDBytes(proposalID)...)
}

// VoteDelegationBackfillProgressKey returns the key for a proposal's delegation-tracking backfill cursor.
func VoteDelegationBackfillProgressKey(proposalID uint64) []byte {
	return append(VoteDelegationBackfillProgressKeyPrefix, GetProposalIDBytes(proposalID)...)
}

// VoteDelegationUpdateKey returns the key for a deferred delegation snapshot update.
func VoteDelegationUpdateKey(sequence uint64) []byte {
	return append(VoteDelegationUpdatesKeyPrefix, GetProposalIDBytes(sequence)...)
}

// VoterVoteDelegationUpdatesKeyPrefixForAddress returns a voter's deferred-update index prefix.
func VoterVoteDelegationUpdatesKeyPrefixForAddress(voterAddr sdk.AccAddress) []byte {
	return append(VoterVoteDelegationUpdatesKeyPrefix, address.MustLengthPrefix(voterAddr.Bytes())...)
}

// VoterVoteDelegationUpdateKey returns a voter's deferred-update index key.
func VoterVoteDelegationUpdateKey(voterAddr sdk.AccAddress, sequence uint64) []byte {
	return append(VoterVoteDelegationUpdatesKeyPrefixForAddress(voterAddr), GetProposalIDBytes(sequence)...)
}

// VoteDelegationSnapshotRevisionKey returns a vote snapshot's applied-update sequence key.
func VoteDelegationSnapshotRevisionKey(proposalID uint64, voterAddr sdk.AccAddress) []byte {
	return append(append(VoteDelegationSnapshotRevisionKeyPrefix, GetProposalIDBytes(proposalID)...), address.MustLengthPrefix(voterAddr.Bytes())...)
}

// ProposalDeadlineByTimeKey returns the proposal-deadline prefix for an end time.
func ProposalDeadlineByTimeKey(endTime time.Time) []byte {
	return append(ProposalDeadlineKeyPrefix, sdk.FormatTimeBytes(endTime)...)
}

// ProposalDeadlineKey returns the deadline key for one proposal tally round.
func ProposalDeadlineKey(proposalID uint64, endTime time.Time) []byte {
	return append(ProposalDeadlineByTimeKey(endTime), GetProposalIDBytes(proposalID)...)
}

// TallyBoundaryMetaKey returns the frozen electorate key for a boundary identifier.
func TallyBoundaryMetaKey(boundaryID []byte) []byte {
	return append(TallyBoundaryMetaKeyPrefix, boundaryID...)
}

// GapTallyBoundaryKey returns the boundary index for deadlines before a block time.
func GapTallyBoundaryKey(upperTime time.Time) []byte {
	return append(GapTallyBoundaryKeyPrefix, sdk.FormatTimeBytes(upperTime)...)
}

// ExactTallyBoundaryKey returns the boundary index for deadlines equal to a block time.
func ExactTallyBoundaryKey(endTime time.Time) []byte {
	return append(ExactTallyBoundaryKeyPrefix, sdk.FormatTimeBytes(endTime)...)
}

// ProposalTallyBoundaryKey returns the selected-boundary key for a proposal.
func ProposalTallyBoundaryKey(proposalID uint64) []byte {
	return append(ProposalTallyBoundaryKeyPrefix, GetProposalIDBytes(proposalID)...)
}

// ModernTallyRoundKey returns the marker for a post-expedited tally round using deadline semantics.
func ModernTallyRoundKey(proposalID uint64) []byte {
	return append(ModernTallyRoundKeyPrefix, GetProposalIDBytes(proposalID)...)
}

// TallyVotesKey returns the prefix for votes archived during a proposal tally round.
func TallyVotesKey(proposalID uint64, expedited bool) []byte {
	return append(append(TallyVotesKeyPrefix, GetProposalIDBytes(proposalID)...), tallyRound(expedited))
}

// TallyVoteKey returns the key for a vote archived during a proposal tally.
func TallyVoteKey(proposalID uint64, expedited bool, voterAddr sdk.AccAddress) []byte {
	return append(TallyVotesKey(proposalID, expedited), address.MustLengthPrefix(voterAddr.Bytes())...)
}

// TallyVoteDelegationsKey returns the key for an archived vote's delegation snapshot.
func TallyVoteDelegationsKey(proposalID uint64, expedited bool, voterAddr sdk.AccAddress) []byte {
	return append(append(append(TallyVoteDelegationsKeyPrefix, GetProposalIDBytes(proposalID)...), tallyRound(expedited)), address.MustLengthPrefix(voterAddr.Bytes())...)
}

// TallyVoteDelegationsKeyFromVoteKey returns the delegation-snapshot key paired with an archived vote key.
func TallyVoteDelegationsKeyFromVoteKey(voteKey []byte) []byte {
	kv.AssertKeyAtLeastLength(voteKey, 11)
	if voteKey[0] != TallyVotesKeyPrefix[0] {
		panic(fmt.Sprintf("invalid tally vote key prefix %d", voteKey[0]))
	}
	decodeTallyRound(voteKey[9])

	key := append([]byte(nil), voteKey...)
	key[0] = TallyVoteDelegationsKeyPrefix[0]
	return key
}

// TallyCleanupKey returns the key for a proposal tally round's archived-vote cleanup cursor.
func TallyCleanupKey(proposalID uint64, expedited bool) []byte {
	return append(append(TallyCleanupKeyPrefix, GetProposalIDBytes(proposalID)...), tallyRound(expedited))
}

// TallyValidatorAccumulatorsKey returns the prefix for mutable validator state in a tally round.
func TallyValidatorAccumulatorsKey(proposalID uint64, expedited bool) []byte {
	return append(append(TallyValidatorAccumulatorsKeyPrefix, GetProposalIDBytes(proposalID)...), tallyRound(expedited))
}

// TallyValidatorAccumulatorKey returns the key for one validator's mutable tally state.
func TallyValidatorAccumulatorKey(proposalID uint64, expedited bool, validator sdk.ValAddress) []byte {
	return append(TallyValidatorAccumulatorsKey(proposalID, expedited), address.MustLengthPrefix(validator.Bytes())...)
}

// TallyAccumulatorCleanupKey returns the cleanup cursor key for a tally round's validator accumulators.
func TallyAccumulatorCleanupKey(proposalID uint64, expedited bool) []byte {
	return append(append(TallyAccumulatorCleanupKeyPrefix, GetProposalIDBytes(proposalID)...), tallyRound(expedited))
}

func tallyRound(expedited bool) byte {
	if expedited {
		return 0
	}
	return 1
}

func decodeTallyRound(round byte) bool {
	switch round {
	case tallyRound(true):
		return true
	case tallyRound(false):
		return false
	default:
		panic(fmt.Sprintf("invalid tally round %d", round))
	}
}

// Split keys function; used for iterators

// SplitProposalKey split the proposal key and returns the proposal id
func SplitProposalKey(key []byte) (proposalID uint64) {
	kv.AssertKeyLength(key[1:], 8)

	return GetProposalIDFromBytes(key[1:])
}

// SplitActiveProposalQueueKey split the active proposal key and returns the proposal id and endTime
func SplitActiveProposalQueueKey(key []byte) (proposalID uint64, endTime time.Time) {
	return splitKeyWithTime(key)
}

// SplitInactiveProposalQueueKey split the inactive proposal key and returns the proposal id and endTime
func SplitInactiveProposalQueueKey(key []byte) (proposalID uint64, endTime time.Time) {
	return splitKeyWithTime(key)
}

// SplitTallyCleanupKey returns the proposal and tally round encoded in a cleanup key.
func SplitTallyCleanupKey(key []byte) (proposalID uint64, expedited bool) {
	kv.AssertKeyLength(key, 10)
	if key[0] != TallyCleanupKeyPrefix[0] {
		panic(fmt.Sprintf("invalid tally cleanup key prefix %d", key[0]))
	}
	return GetProposalIDFromBytes(key[1:9]), decodeTallyRound(key[9])
}

// SplitTallyAccumulatorCleanupKey returns the proposal and tally round encoded in an accumulator cleanup key.
func SplitTallyAccumulatorCleanupKey(key []byte) (proposalID uint64, expedited bool) {
	kv.AssertKeyLength(key, 10)
	if key[0] != TallyAccumulatorCleanupKeyPrefix[0] {
		panic(fmt.Sprintf("invalid tally accumulator cleanup key prefix %d", key[0]))
	}
	return GetProposalIDFromBytes(key[1:9]), decodeTallyRound(key[9])
}

// SplitKeyDeposit split the deposits key and returns the proposal id and depositor address
func SplitKeyDeposit(key []byte) (proposalID uint64, depositorAddr sdk.AccAddress) {
	return splitKeyWithAddress(key)
}

// SplitKeyVote split the votes key and returns the proposal id and voter address
func SplitKeyVote(key []byte) (proposalID uint64, voterAddr sdk.AccAddress) {
	return splitKeyWithAddress(key)
}

// private functions

func splitKeyWithTime(key []byte) (proposalID uint64, endTime time.Time) {
	kv.AssertKeyLength(key[1:], 8+lenTime)

	endTime, err := sdk.ParseTimeBytes(key[1 : 1+lenTime])
	if err != nil {
		panic(err)
	}

	proposalID = GetProposalIDFromBytes(key[1+lenTime:])
	return
}

func splitKeyWithAddress(key []byte) (proposalID uint64, addr sdk.AccAddress) {
	// Both Vote and Deposit store keys are of format:
	// <prefix (1 Byte)><proposalID (8 bytes)><addrLen (1 Byte)><addr_Bytes>
	kv.AssertKeyAtLeastLength(key, 10)
	proposalID = GetProposalIDFromBytes(key[1:9])
	kv.AssertKeyAtLeastLength(key, 11)
	addr = sdk.AccAddress(key[10:])
	return
}

package simulation

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

// NewDecodeStore returns a decoder function closure that unmarshals the KVPair's
// Value to the corresponding gov type.
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case bytes.Equal(kvA.Key[:1], types.ProposalsKeyPrefix):
			var proposalA types.Proposal
			err := cdc.Unmarshal(kvA.Value, &proposalA)
			if err != nil {
				panic(err)
			}
			var proposalB types.Proposal
			err = cdc.Unmarshal(kvB.Value, &proposalB)
			if err != nil {
				panic(err)
			}
			return fmt.Sprintf("%v\n%v", proposalA, proposalB)

		case bytes.Equal(kvA.Key[:1], types.ActiveProposalQueuePrefix),
			bytes.Equal(kvA.Key[:1], types.InactiveProposalQueuePrefix),
			bytes.Equal(kvA.Key[:1], types.ProposalIDKey):
			proposalIDA := binary.LittleEndian.Uint64(kvA.Value)
			proposalIDB := binary.LittleEndian.Uint64(kvB.Value)
			return fmt.Sprintf("proposalIDA: %d\nProposalIDB: %d", proposalIDA, proposalIDB)

		case bytes.Equal(kvA.Key[:1], types.DepositsKeyPrefix):
			var depositA, depositB types.Deposit
			cdc.MustUnmarshal(kvA.Value, &depositA)
			cdc.MustUnmarshal(kvB.Value, &depositB)
			return fmt.Sprintf("%v\n%v", depositA, depositB)

		case bytes.Equal(kvA.Key[:1], types.VotesKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.TallyVotesKeyPrefix):
			var voteA, voteB types.Vote
			cdc.MustUnmarshal(kvA.Value, &voteA)
			cdc.MustUnmarshal(kvB.Value, &voteB)
			return fmt.Sprintf("%v\n%v", voteA, voteB)

		case bytes.Equal(kvA.Key[:1], types.TallyProgressKeyPrefix):
			var progressA, progressB types.TallyProgress
			cdc.MustUnmarshal(kvA.Value, &progressA)
			cdc.MustUnmarshal(kvB.Value, &progressB)
			return fmt.Sprintf("%v\n%v", progressA, progressB)

		case bytes.Equal(kvA.Key[:1], types.VoteDelegationsKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.TallyVoteDelegationsKeyPrefix):
			var snapshotA, snapshotB types.VoteDelegationSnapshot
			cdc.MustUnmarshal(kvA.Value, &snapshotA)
			cdc.MustUnmarshal(kvB.Value, &snapshotB)
			return fmt.Sprintf("%v\n%v", snapshotA, snapshotB)

		case bytes.Equal(kvA.Key[:1], types.VoteDelegationUpdatesKeyPrefix):
			var updateA, updateB types.VoteDelegationUpdate
			cdc.MustUnmarshal(kvA.Value, &updateA)
			cdc.MustUnmarshal(kvB.Value, &updateB)
			return fmt.Sprintf("%v\n%v", updateA, updateB)

		case bytes.Equal(kvA.Key[:1], types.TallyBoundaryMetaKeyPrefix):
			var boundaryA, boundaryB types.TallyBoundary
			cdc.MustUnmarshal(kvA.Value, &boundaryA)
			cdc.MustUnmarshal(kvB.Value, &boundaryB)
			return fmt.Sprintf("%v\n%v", boundaryA, boundaryB)

		case bytes.Equal(kvA.Key[:1], types.TallyValidatorAccumulatorsKeyPrefix):
			var accumulatorA, accumulatorB types.TallyValidatorAccumulator
			cdc.MustUnmarshal(kvA.Value, &accumulatorA)
			cdc.MustUnmarshal(kvB.Value, &accumulatorB)
			return fmt.Sprintf("%v\n%v", accumulatorA, accumulatorB)

		case bytes.Equal(kvA.Key[:1], types.TallyCleanupKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.VoterProposalsKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.VoteDelegationBackfillCutoffKey),
			bytes.Equal(kvA.Key[:1], types.VoteDelegationBackfillProgressKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.VoteDelegationUpdateSequenceKey),
			bytes.Equal(kvA.Key[:1], types.VoterVoteDelegationUpdatesKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.VoteDelegationSnapshotRevisionKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.ProposalDeadlineKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.DeadlineBoundaryBlockTimeKey),
			bytes.Equal(kvA.Key[:1], types.GapTallyBoundaryKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.ExactTallyBoundaryKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.ProposalTallyBoundaryKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.IncrementalTallyEnabledKey),
			bytes.Equal(kvA.Key[:1], types.ModernTallyRoundKeyPrefix),
			bytes.Equal(kvA.Key[:1], types.TallyAccumulatorCleanupKeyPrefix):
			return fmt.Sprintf("%X\n%X", kvA.Value, kvB.Value)

		default:
			panic(fmt.Sprintf("invalid governance key prefix %X", kvA.Key[:1]))
		}
	}
}

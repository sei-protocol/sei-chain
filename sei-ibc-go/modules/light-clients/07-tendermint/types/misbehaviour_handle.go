package types

import (
	"bytes"
	"time"

	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clienttypes "github.com/cosmos/ibc-go/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/modules/core/exported"
)

// CheckMisbehaviourAndUpdateState determines whether or not two conflicting
// headers at the same height would have convinced the light client.
//
// NOTE: consensusState1 is the trusted consensus state that corresponds to the TrustedHeight
// of misbehaviour.Header1
// Similarly, consensusState2 is the trusted consensus state that corresponds
// to misbehaviour.Header2
// Misbehaviour sets frozen height to {0, 1} since it is only used as a boolean value (zero or non-zero).
func (cs ClientState) CheckMisbehaviourAndUpdateState(
	ctx sdk.Context,
	cdc codec.BinaryCodec,
	clientStore sdk.KVStore,
	misbehaviour exported.Misbehaviour,
) (exported.ClientState, error) {
	tmMisbehaviour, ok := misbehaviour.(*Misbehaviour)
	if !ok {
		return nil, sdkerrors.Wrapf(clienttypes.ErrInvalidClientType, "expected type %T, got %T", misbehaviour, &Misbehaviour{})
	}

	// The status of the client is checked in 02-client

	// if heights are equal check that this is valid misbehaviour of a fork
	// otherwise if heights are unequal check that this is valid misbehavior of BFT time violation
	if tmMisbehaviour.Header1.GetHeight().EQ(tmMisbehaviour.Header2.GetHeight()) {
		blockID1, err := tmtypes.BlockIDFromProto(&tmMisbehaviour.Header1.SignedHeader.Commit.BlockID)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "invalid block ID from header 1 in misbehaviour")
		}
		blockID2, err := tmtypes.BlockIDFromProto(&tmMisbehaviour.Header2.SignedHeader.Commit.BlockID)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "invalid block ID from header 2 in misbehaviour")
		}

		// Ensure that Commit Hashes are different
		if bytes.Equal(blockID1.Hash, blockID2.Hash) {
			return nil, sdkerrors.Wrap(clienttypes.ErrInvalidMisbehaviour, "headers block hashes are equal")
		}
	} else {
		// Header1 is at greater height than Header2, therefore Header1 time must be less than or equal to
		// Header2 time in order to be valid misbehaviour (violation of monotonic time).
		if tmMisbehaviour.Header1.SignedHeader.Header.Time.After(tmMisbehaviour.Header2.SignedHeader.Header.Time) {
			return nil, sdkerrors.Wrap(clienttypes.ErrInvalidMisbehaviour, "headers are not at same height and are monotonically increasing")
		}
	}

	// Regardless of the type of misbehaviour, ensure that both headers are valid and would have been accepted by light-client

	// Retrieve trusted consensus states for each Header in misbehaviour
	// and unmarshal from clientStore

	// Get consensus bytes from clientStore
	tmConsensusState1, err := GetConsensusState(clientStore, cdc, tmMisbehaviour.Header1.TrustedHeight)
	if err != nil {
		return nil, sdkerrors.Wrapf(err, "could not get trusted consensus state from clientStore for Header1 at TrustedHeight: %s", tmMisbehaviour.Header1)
	}

	// Get consensus bytes from clientStore
	tmConsensusState2, err := GetConsensusState(clientStore, cdc, tmMisbehaviour.Header2.TrustedHeight)
	if err != nil {
		return nil, sdkerrors.Wrapf(err, "could not get trusted consensus state from clientStore for Header2 at TrustedHeight: %s", tmMisbehaviour.Header2)
	}

	// Check the validity of the two conflicting headers against their respective
	// trusted consensus states
	// NOTE: header height and commitment root assertions are checked in
	// misbehaviour.ValidateBasic by the client keeper and msg.ValidateBasic
	// by the base application.
	if err := checkMisbehaviourHeader(
		&cs, tmConsensusState1, tmMisbehaviour.Header1, ctx.BlockTime(),
	); err != nil {
		return nil, sdkerrors.Wrap(err, "verifying Header1 in Misbehaviour failed")
	}
	if err := checkMisbehaviourHeader(
		&cs, tmConsensusState2, tmMisbehaviour.Header2, ctx.BlockTime(),
	); err != nil {
		return nil, sdkerrors.Wrap(err, "verifying Header2 in Misbehaviour failed")
	}

	cs.FrozenHeight = FrozenHeight

	return &cs, nil
}

// checkMisbehaviourHeader checks that a Header in Misbehaviour is valid misbehaviour given
// a trusted ConsensusState
func checkMisbehaviourHeader(
	clientState *ClientState, consState *ConsensusState, header *Header, currentTimestamp time.Time,
) error {

	tmTrustedValset, err := tmtypes.ValidatorSetFromProto(header.TrustedValidators)
	if err != nil {
		return sdkerrors.Wrap(err, "trusted validator set is not tendermint validator set type")
	}

	tmCommit, err := tmtypes.CommitFromProto(header.Commit)
	if err != nil {
		return sdkerrors.Wrap(err, "commit is not tendermint commit type")
	}

	// check the trusted fields for the header against ConsensusState
	if err := checkTrustedHeader(header, consState); err != nil {
		return err
	}

	// assert that the age of the trusted consensus state is not older than the trusting period
	if currentTimestamp.Sub(consState.Timestamp) >= clientState.TrustingPeriod {
		return sdkerrors.Wrapf(
			ErrTrustingPeriodExpired,
			"current timestamp minus the latest consensus state timestamp is greater than or equal to the trusting period (%d >= %d)",
			currentTimestamp.Sub(consState.Timestamp), clientState.TrustingPeriod,
		)
	}

	chainID := clientState.GetChainID()
	// If chainID is in revision format, then set revision number of chainID with the revision number
	// of the misbehaviour header
	if clienttypes.IsRevisionFormat(chainID) {
		chainID, _ = clienttypes.SetRevisionNumber(chainID, header.GetHeight().GetRevisionNumber())
	}

	// - ValidatorSet must have TrustLevel similarity with trusted FromValidatorSet
	// - ValidatorSets on both headers are valid given the last trusted ValidatorSet
	if err := tmTrustedValset.VerifyCommitLightTrusting(
		chainID, tmCommit, clientState.TrustLevel.ToTendermint(),
	); err != nil {
		return sdkerrors.Wrapf(clienttypes.ErrInvalidMisbehaviour, "validator set in header has too much change from trusted validator set: %v", err)
	}
	return nil
}

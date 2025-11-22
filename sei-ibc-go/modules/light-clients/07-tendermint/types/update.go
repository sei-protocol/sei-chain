package types

import (
	"bytes"
	"reflect"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/tendermint/tendermint/light"
	tmtypes "github.com/tendermint/tendermint/types"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	commitmenttypes "github.com/cosmos/ibc-go/v3/modules/core/23-commitment/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
)

// CheckHeaderAndUpdateState checks if the provided header is valid, and if valid it will:
// create the consensus state for the header.Height
// and update the client state if the header height is greater than the latest client state height
// It returns an error if:
// - the client or header provided are not parseable to tendermint types
// - the header is invalid
// - header height is less than or equal to the trusted header height
// - header revision is not equal to trusted header revision
// - header valset commit verification fails
// - header timestamp is past the trusting period in relation to the consensus state
// - header timestamp is less than or equal to the consensus state timestamp
//
// UpdateClient may be used to either create a consensus state for:
// - a future height greater than the latest client state height
// - a past height that was skipped during bisection
// If we are updating to a past height, a consensus state is created for that height to be persisted in client store
// If we are updating to a future height, the consensus state is created and the client state is updated to reflect
// the new latest height
// UpdateClient must only be used to update within a single revision, thus header revision number and trusted height's revision
// number must be the same. To update to a new revision, use a separate upgrade path
// Tendermint client validity checking uses the bisection algorithm described
// in the [Tendermint spec](https://github.com/tendermint/spec/blob/master/spec/consensus/light-client.md).
//
// Misbehaviour Detection:
// UpdateClient will detect implicit misbehaviour by enforcing certain invariants on any new update call and will return a frozen client.
// 1. Any valid update that creates a different consensus state for an already existing height is evidence of misbehaviour and will freeze client.
// 2. Any valid update that breaks time monotonicity with respect to its neighboring consensus states is evidence of misbehaviour and will freeze client.
// Misbehaviour sets frozen height to {0, 1} since it is only used as a boolean value (zero or non-zero).
//
// Pruning:
// UpdateClient will additionally retrieve the earliest consensus state for this clientID and check if it is expired. If it is,
// that consensus state will be pruned from store along with all associated metadata. This will prevent the client store from
// becoming bloated with expired consensus states that can no longer be used for updates and packet verification.
func (cs ClientState) CheckHeaderAndUpdateState(
	ctx sdk.Context, cdc codec.BinaryCodec, clientStore sdk.KVStore,
	header exported.Header,
) (exported.ClientState, exported.ConsensusState, error) {
	tmHeader, ok := header.(*Header)
	if !ok {
		return nil, nil, sdkerrors.Wrapf(
			clienttypes.ErrInvalidHeader, "expected type %T, got %T", &Header{}, header,
		)
	}

	// Check if the Client store already has a consensus state for the header's height
	// If the consensus state exists, and it matches the header then we return early
	// since header has already been submitted in a previous UpdateClient.
	var conflictingHeader bool
	prevConsState, _ := GetConsensusState(clientStore, cdc, header.GetHeight())
	if prevConsState != nil {
		// This header has already been submitted and the necessary state is already stored
		// in client store, thus we can return early without further validation.
		if reflect.DeepEqual(prevConsState, tmHeader.ConsensusState()) {
			return &cs, prevConsState, nil
		}
		// A consensus state already exists for this height, but it does not match the provided header.
		// Thus, we must check that this header is valid, and if so we will freeze the client.
		conflictingHeader = true
	}

	// get consensus state from clientStore
	trustedConsState, err := GetConsensusState(clientStore, cdc, tmHeader.TrustedHeight)
	if err != nil {
		return nil, nil, sdkerrors.Wrapf(
			err, "could not get consensus state from clientstore at TrustedHeight: %s", tmHeader.TrustedHeight,
		)
	}

	if err := checkValidity(&cs, trustedConsState, tmHeader, ctx.BlockTime()); err != nil {
		return nil, nil, err
	}

	consState := tmHeader.ConsensusState()
	// Header is different from existing consensus state and also valid, so freeze the client and return
	if conflictingHeader {
		cs.FrozenHeight = FrozenHeight
		return &cs, consState, nil
	}
	// Check that consensus state timestamps are monotonic
	prevCons, prevOk := GetPreviousConsensusState(clientStore, cdc, header.GetHeight())
	nextCons, nextOk := GetNextConsensusState(clientStore, cdc, header.GetHeight())
	// if previous consensus state exists, check consensus state time is greater than previous consensus state time
	// if previous consensus state is not before current consensus state, freeze the client and return.
	if prevOk && !prevCons.Timestamp.Before(consState.Timestamp) {
		cs.FrozenHeight = FrozenHeight
		return &cs, consState, nil
	}
	// if next consensus state exists, check consensus state time is less than next consensus state time
	// if next consensus state is not after current consensus state, freeze the client and return.
	if nextOk && !nextCons.Timestamp.After(consState.Timestamp) {
		cs.FrozenHeight = FrozenHeight
		return &cs, consState, nil
	}

	// Check the earliest consensus state to see if it is expired, if so then set the prune height
	// so that we can delete consensus state and all associated metadata.
	var (
		pruneHeight exported.Height
		pruneError  error
	)
	pruneCb := func(height exported.Height) bool {
		consState, err := GetConsensusState(clientStore, cdc, height)
		// this error should never occur
		if err != nil {
			pruneError = err
			return true
		}
		if cs.IsExpired(consState.Timestamp, ctx.BlockTime()) {
			pruneHeight = height
		}
		return true
	}
	IterateConsensusStateAscending(clientStore, pruneCb)
	if pruneError != nil {
		return nil, nil, pruneError
	}
	// if pruneHeight is set, delete consensus state and metadata
	if pruneHeight != nil {
		deleteConsensusState(clientStore, pruneHeight)
		deleteConsensusMetadata(clientStore, pruneHeight)
	}

	newClientState, consensusState := update(ctx, clientStore, &cs, tmHeader)
	return newClientState, consensusState, nil
}

// checkTrustedHeader checks that consensus state matches trusted fields of Header
func checkTrustedHeader(header *Header, consState *ConsensusState) error {
	tmTrustedValidators, err := tmtypes.ValidatorSetFromProto(header.TrustedValidators)
	if err != nil {
		return sdkerrors.Wrap(err, "trusted validator set in not tendermint validator set type")
	}

	// assert that trustedVals is NextValidators of last trusted header
	// to do this, we check that trustedVals.Hash() == consState.NextValidatorsHash
	tvalHash := tmTrustedValidators.Hash()
	if !bytes.Equal(consState.NextValidatorsHash, tvalHash) {
		return sdkerrors.Wrapf(
			ErrInvalidValidatorSet,
			"trusted validators %s, does not hash to latest trusted validators. Expected: %X, got: %X",
			header.TrustedValidators, consState.NextValidatorsHash, tvalHash,
		)
	}
	return nil
}

// checkValidity checks if the Tendermint header is valid.
// CONTRACT: consState.Height == header.TrustedHeight
func checkValidity(
	clientState *ClientState, consState *ConsensusState,
	header *Header, currentTimestamp time.Time,
) error {
	if err := checkTrustedHeader(header, consState); err != nil {
		return err
	}

	// UpdateClient only accepts updates with a header at the same revision
	// as the trusted consensus state
	if header.GetHeight().GetRevisionNumber() != header.TrustedHeight.RevisionNumber {
		return sdkerrors.Wrapf(
			ErrInvalidHeaderHeight,
			"header height revision %d does not match trusted header revision %d",
			header.GetHeight().GetRevisionNumber(), header.TrustedHeight.RevisionNumber,
		)
	}

	tmTrustedValidators, err := tmtypes.ValidatorSetFromProto(header.TrustedValidators)
	if err != nil {
		return sdkerrors.Wrap(err, "trusted validator set in not tendermint validator set type")
	}

	tmSignedHeader, err := tmtypes.SignedHeaderFromProto(header.SignedHeader)
	if err != nil {
		return sdkerrors.Wrap(err, "signed header in not tendermint signed header type")
	}

	tmValidatorSet, err := tmtypes.ValidatorSetFromProto(header.ValidatorSet)
	if err != nil {
		return sdkerrors.Wrap(err, "validator set in not tendermint validator set type")
	}

	// assert header height is newer than consensus state
	if header.GetHeight().LTE(header.TrustedHeight) {
		return sdkerrors.Wrapf(
			clienttypes.ErrInvalidHeader,
			"header height ≤ consensus state height (%s ≤ %s)", header.GetHeight(), header.TrustedHeight,
		)
	}

	chainID := clientState.GetChainID()
	// If chainID is in revision format, then set revision number of chainID with the revision number
	// of the header we are verifying
	// This is useful if the update is at a previous revision rather than an update to the latest revision
	// of the client.
	// The chainID must be set correctly for the previous revision before attempting verification.
	// Updates for previous revisions are not supported if the chainID is not in revision format.
	if clienttypes.IsRevisionFormat(chainID) {
		chainID, _ = clienttypes.SetRevisionNumber(chainID, header.GetHeight().GetRevisionNumber())
	}

	// Construct a trusted header using the fields in consensus state
	// Only Height, Time, and NextValidatorsHash are necessary for verification
	trustedHeader := tmtypes.Header{
		ChainID:            chainID,
		Height:             int64(header.TrustedHeight.RevisionHeight),
		Time:               consState.Timestamp,
		NextValidatorsHash: consState.NextValidatorsHash,
	}
	signedHeader := tmtypes.SignedHeader{
		Header: &trustedHeader,
	}

	// Verify next header with the passed-in trustedVals
	// - asserts trusting period not passed
	// - assert header timestamp is not past the trusting period
	// - assert header timestamp is past latest stored consensus state timestamp
	// - assert that a TrustLevel proportion of TrustedValidators signed new Commit
	err = light.Verify(
		&signedHeader,
		tmTrustedValidators, tmSignedHeader, tmValidatorSet,
		clientState.TrustingPeriod, currentTimestamp, clientState.MaxClockDrift, clientState.TrustLevel.ToTendermint(),
	)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to verify header")
	}
	return nil
}

// update the consensus state from a new header and set processed time metadata
func update(ctx sdk.Context, clientStore sdk.KVStore, clientState *ClientState, header *Header) (*ClientState, *ConsensusState) {
	height := header.GetHeight().(clienttypes.Height)
	if height.GT(clientState.LatestHeight) {
		clientState.LatestHeight = height
	}
	consensusState := &ConsensusState{
		Timestamp:          header.GetTime(),
		Root:               commitmenttypes.NewMerkleRoot(header.Header.GetAppHash()),
		NextValidatorsHash: header.Header.NextValidatorsHash,
	}

	// set metadata for this consensus state
	setConsensusMetadata(ctx, clientStore, header.GetHeight())

	return clientState, consensusState
}

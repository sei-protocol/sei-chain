package types

import (
	"reflect"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
)

// CheckSubstituteAndUpdateState will try to update the client with the state of the
// substitute if and only if the proposal passes and one of the following conditions are
// satisfied:
//	1) AllowUpdateAfterMisbehaviour and Status() == Frozen
// 	2) AllowUpdateAfterExpiry=true and Status() == Expired
//
// The following must always be true:
//	- The substitute client is the same type as the subject client
//	- The subject and substitute client states match in all parameters (expect frozen height, latest height, and chain-id)
//
// In case 1) before updating the client, the client will be unfrozen by resetting
// the FrozenHeight to the zero Height. If a client is frozen and AllowUpdateAfterMisbehaviour
// is set to true, the client will be unexpired even if AllowUpdateAfterExpiry is set to false.
func (cs ClientState) CheckSubstituteAndUpdateState(
	ctx sdk.Context, cdc codec.BinaryCodec, subjectClientStore,
	substituteClientStore sdk.KVStore, substituteClient exported.ClientState,
) (exported.ClientState, error) {
	substituteClientState, ok := substituteClient.(*ClientState)
	if !ok {
		return nil, sdkerrors.Wrapf(
			clienttypes.ErrInvalidClient, "expected type %T, got %T", &ClientState{}, substituteClient,
		)
	}

	if !IsMatchingClientState(cs, *substituteClientState) {
		return nil, sdkerrors.Wrap(clienttypes.ErrInvalidSubstitute, "subject client state does not match substitute client state")
	}

	switch cs.Status(ctx, subjectClientStore, cdc) {

	case exported.Frozen:
		if !cs.AllowUpdateAfterMisbehaviour {
			return nil, sdkerrors.Wrap(clienttypes.ErrUpdateClientFailed, "client is not allowed to be unfrozen")
		}

		// unfreeze the client
		cs.FrozenHeight = clienttypes.ZeroHeight()

	case exported.Expired:
		if !cs.AllowUpdateAfterExpiry {
			return nil, sdkerrors.Wrap(clienttypes.ErrUpdateClientFailed, "client is not allowed to be unexpired")
		}

	default:
		return nil, sdkerrors.Wrap(clienttypes.ErrUpdateClientFailed, "client cannot be updated with proposal")
	}

	// copy consensus states and processed time from substitute to subject
	// starting from initial height and ending on the latest height (inclusive)
	height := substituteClientState.GetLatestHeight()

	consensusState, err := GetConsensusState(substituteClientStore, cdc, height)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "unable to retrieve latest consensus state for substitute client")
	}

	SetConsensusState(subjectClientStore, cdc, consensusState, height)

	// set metadata stored for the substitute consensus state
	processedHeight, found := GetProcessedHeight(substituteClientStore, height)
	if !found {
		return nil, sdkerrors.Wrap(clienttypes.ErrUpdateClientFailed, "unable to retrieve processed height for substitute client latest height")
	}

	processedTime, found := GetProcessedTime(substituteClientStore, height)
	if !found {
		return nil, sdkerrors.Wrap(clienttypes.ErrUpdateClientFailed, "unable to retrieve processed time for substitute client latest height")
	}

	setConsensusMetadataWithValues(subjectClientStore, height, processedHeight, processedTime)

	cs.LatestHeight = substituteClientState.LatestHeight
	cs.ChainId = substituteClientState.ChainId

	// no validation is necessary since the substitute is verified to be Active
	// in 02-client.

	return &cs, nil
}

// IsMatchingClientState returns true if all the client state parameters match
// except for frozen height, latest height, and chain-id.
func IsMatchingClientState(subject, substitute ClientState) bool {
	// zero out parameters which do not need to match
	subject.LatestHeight = clienttypes.ZeroHeight()
	subject.FrozenHeight = clienttypes.ZeroHeight()
	substitute.LatestHeight = clienttypes.ZeroHeight()
	substitute.FrozenHeight = clienttypes.ZeroHeight()
	subject.ChainId = ""
	substitute.ChainId = ""

	return reflect.DeepEqual(subject, substitute)
}

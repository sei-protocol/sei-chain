package types

import (
	"fmt"
	"strconv"
	"strings"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	porttypes "github.com/cosmos/ibc-go/v3/modules/core/05-port/types"
)

const (
	// ControllerPortFormat is the expected port identifier format to which controller chains must conform
	// See (TODO: Link to spec when updated)
	ControllerPortFormat = "<app-version>.<controller-conn-seq>.<host-conn-seq>.<owner>"
)

// GeneratePortID generates an interchain accounts controller port identifier for the provided owner
// in the following format:
//
// 'ics-27-<connectionSequence>-<counterpartyConnectionSequence>-<owner-address>'
// https://github.com/seantking/ibc/tree/sean/ics-27-updates/spec/app/ics-027-interchain-accounts#registering--controlling-flows
// TODO: update link to spec
func GeneratePortID(owner, connectionID, counterpartyConnectionID string) (string, error) {
	if strings.TrimSpace(owner) == "" {
		return "", sdkerrors.Wrap(ErrInvalidAccountAddress, "owner address cannot be empty")
	}

	connectionSeq, err := connectiontypes.ParseConnectionSequence(connectionID)
	if err != nil {
		return "", sdkerrors.Wrap(err, "invalid connection identifier")
	}

	counterpartyConnectionSeq, err := connectiontypes.ParseConnectionSequence(counterpartyConnectionID)
	if err != nil {
		return "", sdkerrors.Wrap(err, "invalid counterparty connection identifier")
	}

	return fmt.Sprint(
		VersionPrefix, Delimiter,
		connectionSeq, Delimiter,
		counterpartyConnectionSeq, Delimiter,
		owner,
	), nil
}

// ParseControllerConnSequence attempts to parse the controller connection sequence from the provided port identifier
// The port identifier must match the controller chain format outlined in (TODO: link spec), otherwise an empty string is returned
func ParseControllerConnSequence(portID string) (uint64, error) {
	s := strings.Split(portID, Delimiter)
	if len(s) != 4 {
		return 0, sdkerrors.Wrap(porttypes.ErrInvalidPort, "failed to parse port identifier")
	}

	seq, err := strconv.ParseUint(s[1], 10, 64)
	if err != nil {
		return 0, sdkerrors.Wrapf(err, "failed to parse connection sequence (%s)", s[1])
	}

	return seq, nil
}

// ParseHostConnSequence attempts to parse the host connection sequence from the provided port identifier
// The port identifier must match the controller chain format outlined in (TODO: link spec), otherwise an empty string is returned
func ParseHostConnSequence(portID string) (uint64, error) {
	s := strings.Split(portID, Delimiter)
	if len(s) != 4 {
		return 0, sdkerrors.Wrap(porttypes.ErrInvalidPort, "failed to parse port identifier")
	}

	seq, err := strconv.ParseUint(s[2], 10, 64)
	if err != nil {
		return 0, sdkerrors.Wrapf(err, "failed to parse connection sequence (%s)", s[2])
	}

	return seq, nil
}

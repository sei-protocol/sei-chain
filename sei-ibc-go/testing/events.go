package ibctesting

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	abci "github.com/tendermint/tendermint/abci/types"
	"slices"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
)

// ParseClientIDFromEvents parses events emitted from a MsgCreateClient and returns the
// client identifier.
func ParseClientIDFromEvents(events sdk.Events) (string, error) {
	for _, ev := range events {
		if ev.Type == clienttypes.EventTypeCreateClient {
			for _, attr := range ev.Attributes {
				if string(attr.Key) == clienttypes.AttributeKeyClientID {
					return string(attr.Value), nil
				}
			}
		}
	}
	return "", fmt.Errorf("client identifier event attribute not found")
}

// ParseConnectionIDFromEvents parses events emitted from a MsgConnectionOpenInit or
// MsgConnectionOpenTry and returns the connection identifier.
func ParseConnectionIDFromEvents(events sdk.Events) (string, error) {
	for _, ev := range events {
		if ev.Type == connectiontypes.EventTypeConnectionOpenInit ||
			ev.Type == connectiontypes.EventTypeConnectionOpenTry {
			for _, attr := range ev.Attributes {
				if string(attr.Key) == connectiontypes.AttributeKeyConnectionID {
					return string(attr.Value), nil
				}
			}
		}
	}
	return "", fmt.Errorf("connection identifier event attribute not found")
}

// ParseChannelIDFromEvents parses events emitted from a MsgChannelOpenInit or
// MsgChannelOpenTry and returns the channel identifier.
func ParseChannelIDFromEvents(events sdk.Events) (string, error) {
	for _, ev := range events {
		if ev.Type == channeltypes.EventTypeChannelOpenInit || ev.Type == channeltypes.EventTypeChannelOpenTry {
			for _, attr := range ev.Attributes {
				if string(attr.Key) == channeltypes.AttributeKeyChannelID {
					return string(attr.Value), nil
				}
			}
		}
	}
	return "", fmt.Errorf("channel identifier event attribute not found")
}

// ParsePacketFromEvents parses events emitted from a MsgRecvPacket and returns the
// acknowledgement.
func ParsePacketFromEvents(events sdk.Events) (channeltypes.Packet, error) {
	for _, ev := range events {
		if ev.Type == channeltypes.EventTypeSendPacket {
			packet := channeltypes.Packet{}
			for _, attr := range ev.Attributes {
				switch string(attr.Key) {
				case channeltypes.AttributeKeyData:
					packet.Data = attr.Value

				case channeltypes.AttributeKeySequence:
					seq, err := strconv.ParseUint(string(attr.Value), 10, 64)
					if err != nil {
						return channeltypes.Packet{}, err
					}

					packet.Sequence = seq

				case channeltypes.AttributeKeySrcPort:
					packet.SourcePort = string(attr.Value)

				case channeltypes.AttributeKeySrcChannel:
					packet.SourceChannel = string(attr.Value)

				case channeltypes.AttributeKeyDstPort:
					packet.DestinationPort = string(attr.Value)

				case channeltypes.AttributeKeyDstChannel:
					packet.DestinationChannel = string(attr.Value)

				case channeltypes.AttributeKeyTimeoutHeight:
					height, err := clienttypes.ParseHeight(string(attr.Value))
					if err != nil {
						return channeltypes.Packet{}, err
					}

					packet.TimeoutHeight = height

				case channeltypes.AttributeKeyTimeoutTimestamp:
					timestamp, err := strconv.ParseUint(string(attr.Value), 10, 64)
					if err != nil {
						return channeltypes.Packet{}, err
					}

					packet.TimeoutTimestamp = timestamp

				default:
					continue
				}
			}

			return packet, nil
		}
	}
	return channeltypes.Packet{}, fmt.Errorf("acknowledgement event attribute not found")
}

// ParseAckFromEvents parses events emitted from a MsgRecvPacket and returns the
// acknowledgement.
func ParseAckFromEvents(events sdk.Events) ([]byte, error) {
	for _, ev := range events {
		if ev.Type == channeltypes.EventTypeWriteAck {
			for _, attr := range ev.Attributes {
				if string(attr.Key) == channeltypes.AttributeKeyAck {
					return attr.Value, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("acknowledgement event attribute not found")
}

// AssertEvents asserts that expected events are present in the actual events.
func AssertEvents(
	t assert.TestingT,
	expected []abci.Event,
	actual []abci.Event,
) {
	foundEvents := make(map[int]bool)

	for i, expectedEvent := range expected {
		for _, actualEvent := range actual {
			if shouldProcessEvent(expectedEvent, actualEvent) {
				attributeMatch := true
				for _, expectedAttr := range expectedEvent.Attributes {
					// any expected attributes that are not contained in the actual events will cause this event
					// not to match
					attributeMatch = attributeMatch && containsAttribute(actualEvent.Attributes, string(expectedAttr.Key), string(expectedAttr.Value))
				}

				if attributeMatch {
					foundEvents[i] = true
				}
			}
		}
	}

	for i, expectedEvent := range expected {
		assert.True(t, foundEvents[i], "event: %s was not found in events", expectedEvent.Type)
	}
}

// shouldProcessEvent returns true if the given expected event should be processed based on event type.
func shouldProcessEvent(expectedEvent abci.Event, actualEvent abci.Event) bool {
	if expectedEvent.Type != actualEvent.Type {
		return false
	}
	// the actual event will have an extra attribute added automatically
	// by Cosmos SDK since v0.50, that's why we subtract 1 when comparing
	// with the number of attributes in the expected event.
	if containsAttributeKey(actualEvent.Attributes, "msg_index") {
		return len(expectedEvent.Attributes) == len(actualEvent.Attributes)-1
	}

	return len(expectedEvent.Attributes) == len(actualEvent.Attributes)
}

// containsAttribute returns true if the given key/value pair is contained in the given attributes.
// NOTE: this ignores the indexed field, which can be set or unset depending on how the events are retrieved.
func containsAttribute(attrs []abci.EventAttribute, key, value string) bool {
	return slices.ContainsFunc(attrs, func(attr abci.EventAttribute) bool {
		return string(attr.Key) == key && string(attr.Value) == value
	})
}

// containsAttributeKey returns true if the given key is contained in the given attributes.
func containsAttributeKey(attrs []abci.EventAttribute, key string) bool {
	_, found := attributeByKey(attrs, key)
	return found
}

// attributeByKey returns the event attribute's value keyed by the given key and a boolean indicating its presence in the given attributes.
func attributeByKey(attributes []abci.EventAttribute, key string) (abci.EventAttribute, bool) {
	idx := slices.IndexFunc(attributes, func(a abci.EventAttribute) bool { return string(a.Key) == key })
	if idx == -1 {
		return abci.EventAttribute{}, false
	}
	return attributes[idx], true
}

package types

import (
	"encoding/json"
	"fmt"

	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
)

type WasmMessageInfo struct {
	MessageType     acltypes.WasmMessageSubtype
	MessageName     string
	MessageBody     []byte
	MessageFullBody []byte
}

func NewExecuteMessageInfo(fullBody []byte) (*WasmMessageInfo, error) {
	return newMessageInfo(fullBody, acltypes.WasmMessageSubtype_EXECUTE)
}

func NewQueryMessageInfo(fullBody []byte) (*WasmMessageInfo, error) {
	return newMessageInfo(fullBody, acltypes.WasmMessageSubtype_QUERY)
}

func newMessageInfo(fullBody []byte, messageType acltypes.WasmMessageSubtype) (*WasmMessageInfo, error) {
	name, body, err := extractMessage(fullBody)
	if err != nil {
		return nil, err
	}
	return &WasmMessageInfo{
		MessageType:     messageType,
		MessageName:     name,
		MessageBody:     body,
		MessageFullBody: fullBody,
	}, nil
}

// WASM message body is JSON-serialized and use the message name
// as the only top-level key
func extractMessage(fullBody []byte) (string, []byte, error) {
	var deserialized map[string]json.RawMessage
	if err := json.Unmarshal(fullBody, &deserialized); err != nil {
		return "", fullBody, err
	}
	topLevelKeys := []string{}
	for k := range deserialized {
		topLevelKeys = append(topLevelKeys, k)
	}
	if len(topLevelKeys) != 1 {
		return "", fullBody, fmt.Errorf("expected exactly one top-level key but found %s", topLevelKeys)
	}
	return topLevelKeys[0], deserialized[topLevelKeys[0]], nil
}

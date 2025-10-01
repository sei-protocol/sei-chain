package types

import (
	"encoding/json"
	"fmt"

	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/savaki/jq"
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

type WasmMessageTranslator struct {
	originalSender          string
	originalContractAddress string
	originalMsgInfo         *WasmMessageInfo
}

func NewWasmMessageTranslator(sender, contractAddress string, msgInfo *WasmMessageInfo) WasmMessageTranslator {
	return WasmMessageTranslator{
		originalSender:          sender,
		originalContractAddress: contractAddress,
		originalMsgInfo:         msgInfo,
	}
}

// This function takes in a a translation template formatted as a JSON body and stored as part of a wasm dependency mapping,
// and then applies the JQ style patterns to fill in the template with the appropriate values
// There are some reserved keywords for the template for string values such as sender or contract address for the new JSON message body
//
// "_sender": This is used to fill in the sender for the previous wasm message
//
// "_contract_address": This is used to fill in the contract address for the previous wasm message
//
// "__": This is used to prefix a value literal. eg. "__someValue" -> "someValue"
func (translator WasmMessageTranslator) TranslateMessageBody(translationTemplate []byte) ([]byte, error) {
	jsonTemplate := map[string]interface{}{}
	// parse JSON template map from the bytes
	err := json.Unmarshal(translationTemplate, &jsonTemplate)
	if err != nil {
		return nil, err
	}
	translatedMsgBody := translator.translateMap(jsonTemplate)
	return json.Marshal(translatedMsgBody)
}

func (translator WasmMessageTranslator) translateMap(aMap map[string]interface{}) map[string]interface{} {
	translatedMap := map[string]interface{}{}
	for key, val := range aMap {
		switch concreteVal := val.(type) {
		case map[string]interface{}:
			translatedMap[key] = translator.translateMap(concreteVal)
		case []interface{}:
			translatedMap[key] = translator.translateArray(concreteVal)
		case string:
			translatedString, err := translator.translateValue(concreteVal)
			if err != nil {
				// TODO: how should we handle this? likely incorrectly formatted, I think we should drop so the selectors are evaluated conservatively
				continue
			}
			translatedMap[key] = translatedString
		default:
			translatedMap[key] = concreteVal
		}
	}
	return translatedMap
}

func (translator WasmMessageTranslator) translateArray(anArray []interface{}) []interface{} {
	translatedArray := []interface{}{}
	for _, val := range anArray {
		switch concreteVal := val.(type) {
		case map[string]interface{}:
			translatedArray = append(translatedArray, translator.translateMap(concreteVal))
		case []interface{}:
			translatedArray = append(translatedArray, translator.translateArray(concreteVal))
		case string:
			translatedString, err := translator.translateValue(concreteVal)
			if err != nil {
				// TODO: how should we handle this? likely incorrectly formatted, I think we should drop so the selectors are evaluated conservatively
				continue
			}
			translatedArray = append(translatedArray, translatedString)
		default:
			translatedArray = append(translatedArray, concreteVal)
		}
	}
	return translatedArray
}

func (translator WasmMessageTranslator) translateValue(stringVal string) (interface{}, error) {
	const reservedSender = "_sender"
	const reservedContractAddr = "_contract_address"
	const literalPrefix = "__"
	var newVal interface{}
	if stringVal == reservedSender {
		newVal = translator.originalSender
	} else if stringVal == reservedContractAddr {
		newVal = translator.originalContractAddress
	} else if len(stringVal) > 2 && stringVal[:2] == literalPrefix {
		// fill in the literal without the prefix
		newVal = stringVal[2:]
	} else {
		// parse the jq instruction from the template
		op, err := jq.Parse(stringVal)
		if err != nil {
			return "", err
		}
		// retrieve the appropriate item from the original msg
		data, err := op.Apply(translator.originalMsgInfo.MessageFullBody)
		if err != nil {
			return "", err
		}
		newVal = json.RawMessage(data)
	}
	return newVal, nil
}

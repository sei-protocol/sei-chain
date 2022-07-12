package wasmbinding

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type CustomMessage struct {
	Raw string `json:"raw"`
}

type RawSdkMessages struct {
	Messages []RawMessage `json:"messages"`
}

type RawMessage struct {
	MsgType string `json:"msg_type"`
	Data    string `json:"data"`
}

func CustomEncoder(sender sdk.AccAddress, msg json.RawMessage) ([]sdk.Msg, error) {
	customMsg := CustomMessage{}
	if err := json.Unmarshal(msg, &customMsg); err != nil {
		return []sdk.Msg{}, err
	}
	data, err := base64.StdEncoding.DecodeString(customMsg.Raw)
	if err != nil {
		panic(err)
	}
	return decodeRawSdkMessages(data)
}

func decodeRawSdkMessages(rawMsg []byte) ([]sdk.Msg, error) {
	messages := RawSdkMessages{}
	if err := json.Unmarshal(rawMsg, &messages); err != nil {
		return []sdk.Msg{}, err
	}
	response := []sdk.Msg{}
	for _, message := range messages.Messages {
		msg, err := decodeRawSdkMessage(message)
		if err != nil {
			return response, err
		}
		response = append(response, msg)
	}
	return response, nil
}

func decodeRawSdkMessage(message RawMessage) (sdk.Msg, error) {
	switch message.MsgType {
	case types.TypeMsgPlaceOrders:
		return decodeOrderPlacementMessage(message.Data)
	case types.TypeMsgCancelOrders:
		return decodeOrderCancellationMessage(message.Data)
	default:
		return nil, fmt.Errorf("unknown message type %s", message.MsgType)
	}
}

func decodeOrderPlacementMessage(data string) (sdk.Msg, error) {
	decodedData, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	message := types.MsgPlaceOrders{}
	if err := json.Unmarshal(decodedData, &message); err != nil {
		return nil, err
	}
	return &message, nil
}

func decodeOrderCancellationMessage(data string) (sdk.Msg, error) {
	decodedData, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	message := types.MsgCancelOrders{}
	if err := json.Unmarshal(decodedData, &message); err != nil {
		return nil, err
	}
	return &message, nil
}

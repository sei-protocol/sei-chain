package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractMessage(t *testing.T) {
	goodBody := []byte("{\"key\":{\"val\":{}}}")
	name, body, err := extractMessage(goodBody)
	require.Nil(t, err)
	require.Equal(t, "key", name)
	require.Equal(t, "{\"val\":{}}", string(body))

	badJson := []byte("{\"key\":}")
	_, _, err = extractMessage(badJson)
	require.NotNil(t, err)

	emptyBody := []byte("{}")
	_, _, err = extractMessage(emptyBody)
	require.NotNil(t, err)

	multiKeyBody := []byte("{\"key1\":{},\"key2\":{}}")
	_, _, err = extractMessage(multiKeyBody)
	require.NotNil(t, err)
}

func TestWasmJsonNullTemplate(t *testing.T) {
	info, err := NewExecuteMessageInfo([]byte("{\"send\":{\"from\":\"bobAddress\",\"amount\":10}}"))
	require.NoError(t, err)
	translator := NewWasmMessageTranslator("senderAddress", "contractAddress", info)

	// lets test a simple case where we want to make a new message to perform a "swap"
	jsonTranslationTemplate := []byte("null")

	expectedJson := []byte("{}")
	outputJson, err := translator.TranslateMessageBody(jsonTranslationTemplate)
	require.NoError(t, err)
	require.Equal(t, expectedJson, outputJson)

	_, err = translator.TranslateMessageBody([]byte{})
	require.Error(t, err)
}

func TestWasmJsonTranslator(t *testing.T) {
	info, err := NewExecuteMessageInfo([]byte("{\"send\":{\"from\":\"bobAddress\",\"amount\":10,\"something_else\":null}}"))
	require.NoError(t, err)
	translator := NewWasmMessageTranslator("senderAddress", "contractAddress", info)

	// lets test a simple case where we want to make a new message to perform a "swap"
	jsonTranslationTemplate := []byte("{\"swap\":{\"a\":\"_sender\",\"b\":\"_contract_address\",\"c\":\".send.from\",\"d\":\".doesnt.match\",\"e\":\"*2gibberish\",\"f\":\"__.swap.from\",\"g\":true,\"h\":\".send.amount\",\"i\":null,\"j\":\".send.something_else\"}}")

	expectedJson := []byte("{\"swap\":{\"a\":\"senderAddress\",\"b\":\"contractAddress\",\"c\":\"bobAddress\",\"f\":\".swap.from\",\"g\":true,\"h\":10,\"i\":null,\"j\":null}}")
	outputJson, err := translator.TranslateMessageBody(jsonTranslationTemplate)
	require.NoError(t, err)
	require.Equal(t, expectedJson, outputJson)
}

func TestWasmJsonTranslatorArray(t *testing.T) {
	info, err := NewExecuteMessageInfo([]byte("{\"send\":{\"from\":\"bobAddress\",\"amount\":10}}"))
	require.NoError(t, err)
	translator := NewWasmMessageTranslator("senderAddress", "contractAddress", info)

	// lets test a simple case where we want to make a new message to perform a "swap"
	jsonTranslationTemplate := []byte("{\"swap\":[\"_sender\",\"_contract_address\",\"__sender\",\".send.from\",[\".send.amount\"],{\"some_field\": \"_sender\"}]}")

	expectedJson := []byte("{\"swap\":[\"senderAddress\",\"contractAddress\",\"sender\",\"bobAddress\",[10],{\"some_field\":\"senderAddress\"}]}")
	outputJson, err := translator.TranslateMessageBody(jsonTranslationTemplate)
	require.NoError(t, err)
	require.Equal(t, expectedJson, outputJson)
}

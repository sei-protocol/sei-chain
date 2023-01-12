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

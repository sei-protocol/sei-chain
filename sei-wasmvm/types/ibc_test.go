package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIbcTimeoutSerialization(t *testing.T) {
	// All set
	timeout := IBCTimeout{
		Block: &IBCTimeoutBlock{
			Revision: 17,
			Height:   42,
		},
		Timestamp: 1578939743_987654321,
	}
	bz, err := json.Marshal(timeout)
	require.NoError(t, err)
	assert.Equal(t, `{"block":{"revision":17,"height":42},"timestamp":"1578939743987654321"}`, string(bz))

	// Null block
	timeout = IBCTimeout{
		Block:     nil,
		Timestamp: 1578939743_987654321,
	}
	bz, err = json.Marshal(timeout)
	require.NoError(t, err)
	assert.Equal(t, `{"block":null,"timestamp":"1578939743987654321"}`, string(bz))

	// Null timestamp
	// This should be `"timestamp":null`, but we are lacking this feature: https://github.com/golang/go/issues/37711
	// However, this is good enough right now because in Rust a missing field is deserialized as `None` into `Option<Timestamp>`
	timeout = IBCTimeout{
		Block: &IBCTimeoutBlock{
			Revision: 17,
			Height:   42,
		},
		Timestamp: 0,
	}
	bz, err = json.Marshal(timeout)
	require.NoError(t, err)
	assert.Equal(t, `{"block":{"revision":17,"height":42}}`, string(bz))
}

func TestIbcTimeoutDeserialization(t *testing.T) {
	var err error

	// All set
	var timeout1 IBCTimeout
	err = json.Unmarshal([]byte(`{"block":{"revision":17,"height":42},"timestamp":"1578939743987654321"}`), &timeout1)
	require.NoError(t, err)
	assert.Equal(t, IBCTimeout{
		Block: &IBCTimeoutBlock{
			Revision: 17,
			Height:   42,
		},
		Timestamp: 1578939743_987654321,
	}, timeout1)

	// Null block
	var timeout2 IBCTimeout
	err = json.Unmarshal([]byte(`{"block":null,"timestamp":"1578939743987654321"}`), &timeout2)
	require.NoError(t, err)
	assert.Equal(t, IBCTimeout{
		Block:     nil,
		Timestamp: 1578939743_987654321,
	}, timeout2)

	// Null timestamp
	var timeout3 IBCTimeout
	err = json.Unmarshal([]byte(`{"block":{"revision":17,"height":42},"timestamp":null}`), &timeout3)
	require.NoError(t, err)
	assert.Equal(t, IBCTimeout{
		Block: &IBCTimeoutBlock{
			Revision: 17,
			Height:   42,
		},
		Timestamp: 0,
	}, timeout3)
}

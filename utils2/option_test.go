package utils

import (
	"encoding/json"
	"testing"

	"github.com/sei-protocol/sei-stream/pkg/require"
)

func testJSON[T any](t *testing.T, want T) {
	enc, err := json.Marshal(want)
	require.NoError(t, err)
	t.Logf("%s", enc)
	var got T
	require.NoError(t, json.Unmarshal(enc, &got))
	require.NoError(t, TestDiff(want, got))
}

func TestOptionJSON(t *testing.T) {
	type a struct {
		X Option[int]
		Y Option[string]
	}
	type b struct {
		X Option[int]    `json:"X,omitzero"`
		Y Option[string] `json:"Y,omitzero"`
	}
	testJSON(t, &a{})
	testJSON(t, &a{Some(1), Some("a")})
	testJSON(t, &b{})
	testJSON(t, &b{Some(1), Some("a")})
}

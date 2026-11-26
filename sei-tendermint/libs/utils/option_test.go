package utils

import (
	"encoding/json"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func testJSON[T any](t *testing.T, want T) {
	enc, err := json.Marshal(want)
	require.NoError(t, err)
	t.Logf("%s", enc)
	var got T
	require.NoError(t, json.Unmarshal(enc, &got))
	require.NoError(t, TestDiff(want, got))
}

func TestOptionOrWith(t *testing.T) {
	for _, tc := range []struct {
		name      string
		option    Option[int]
		want      int
		wantCalls int
	}{
		{name: "present", option: Some(7), want: 7},
		{name: "present zero", option: Some(0), want: 0},
		{name: "absent", option: None[int](), want: 42, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got := tc.option.OrWith(func() int {
				calls++
				return 42
			})
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantCalls, calls)
		})
	}
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

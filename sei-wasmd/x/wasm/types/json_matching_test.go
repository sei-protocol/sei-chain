package types

import (
	"testing"

	// sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestIsJSONObjectWithTopLevelKey(t *testing.T) {
	specs := map[string]struct {
		src         []byte
		allowedKeys []string
		exp         error
	}{
		"happy": {
			src:         []byte(`{"msg": {"foo":"bar"}}`),
			allowedKeys: []string{"msg"},
			exp:         nil,
		},
		"happy with many allowed keys 1": {
			src:         []byte(`{"claim": {"foo":"bar"}}`),
			allowedKeys: []string{"claim", "swap", "burn", "mint"},
			exp:         nil,
		},
		"happy with many allowed keys 2": {
			src:         []byte(`{"burn": {"foo":"bar"}}`),
			allowedKeys: []string{"claim", "swap", "burn", "mint"},
			exp:         nil,
		},
		"happy with many allowed keys 3": {
			src:         []byte(`{"mint": {"foo":"bar"}}`),
			allowedKeys: []string{"claim", "swap", "burn", "mint"},
			exp:         nil,
		},
		"happy with number": {
			src:         []byte(`{"msg": 123}`),
			allowedKeys: []string{"msg"},
			exp:         nil,
		},
		"happy with array": {
			src:         []byte(`{"msg": [1, 2, 3, 4]}`),
			allowedKeys: []string{"msg"},
			exp:         nil,
		},
		"happy with null": {
			src:         []byte(`{"msg": null}`),
			allowedKeys: []string{"msg"},
			exp:         nil,
		},
		"happy with whitespace": {
			src: []byte(`{
				"msg":	null    }`),
			allowedKeys: []string{"msg"},
			exp:         nil,
		},
		"happy with excaped key": {
			src:         []byte(`{"event\u2468thing": {"foo":"bar"}}`),
			allowedKeys: []string{"event⑨thing"},
			exp:         nil,
		},

		// Invalid JSON object
		"errors for bytes that are no JSON": {
			src:         []byte(`nope`),
			allowedKeys: []string{"claim"},
			exp:         ErrNotAJSONObject,
		},
		"errors for valid JSON (string)": {
			src:         []byte(`"nope"`),
			allowedKeys: []string{"claim"},
			exp:         ErrNotAJSONObject,
		},
		"errors for valid JSON (array)": {
			src:         []byte(`[1, 2, 3]`),
			allowedKeys: []string{"claim"},
			exp:         ErrNotAJSONObject,
		},

		// Not one top-level key
		"errors for no top-level key": {
			src:         []byte(`{}`),
			allowedKeys: []string{"claim"},
			exp:         ErrNoTopLevelKey,
		},
		"errors for multiple top-level keys": {
			src:         []byte(`{"claim": {}, "and_swap": {}}`),
			allowedKeys: []string{"claim"},
			exp:         ErrMultipleTopLevelKeys,
		},

		// Wrong top-level key
		"errors for wrong top-level key 1": {
			src:         []byte(`{"claim": {}}`),
			allowedKeys: []string{""},
			exp:         ErrTopKevelKeyNotAllowed,
		},
		"errors for wrong top-level key 2": {
			src:         []byte(`{"claim": {}}`),
			allowedKeys: []string{"swap", "burn", "mint"},
			exp:         ErrTopKevelKeyNotAllowed,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			result := IsJSONObjectWithTopLevelKey(spec.src, spec.allowedKeys)
			if spec.exp == nil {
				require.NoError(t, result)
			} else {
				require.Error(t, result)
				require.Contains(t, result.Error(), spec.exp.Error())
			}
		})
	}
}

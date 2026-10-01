package kvrepair

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func evmKey(prefix byte, length int) []byte {
	key := bytes.Repeat([]byte{0x11}, length+1)
	key[0] = prefix
	return key
}

func TestValuesEqualTreatsZeroEVMValuesAsAbsent(t *testing.T) {
	for name, tc := range map[string]struct {
		key  []byte
		zero []byte
	}{
		"storage":   {key: evmKey(0x03, 52), zero: make([]byte, 32)},
		"nonce":     {key: evmKey(0x0a, 20), zero: make([]byte, 8)},
		"code hash": {key: evmKey(0x08, 20), zero: make([]byte, 32)},
		"balance":   {key: evmKey(0x21, 20), zero: make([]byte, 32)},
		"code":      {key: evmKey(0x07, 20), zero: []byte{}},
	} {
		t.Run(name, func(t *testing.T) {
			require.True(t, ReadsAsAbsent("evm", tc.key, tc.zero))
			require.True(t, ValuesEqual("evm", tc.key, nil, tc.zero))
			require.True(t, ValuesEqual("evm", tc.key, tc.zero, nil))
			require.False(t, ValuesEqual("evm", tc.key, nil, []byte{0x01}))
			require.True(t, ValuesEqual("evm", tc.key, []byte{0x01}, []byte{0x01}))
			require.False(t, ValuesEqual("evm", tc.key, []byte{0x01}, []byte{0x02}))
		})
	}
}

func TestValuesEqualKeepsZeroBytecodeDistinctFromAbsent(t *testing.T) {
	key := evmKey(0x07, 20)
	require.False(t, ReadsAsAbsent("evm", key, []byte{0x00}))
	require.False(t, ValuesEqual("evm", key, nil, []byte{0x00}))
}

func TestValuesEqualComparesExactlyOutsideTheEVMKeyKinds(t *testing.T) {
	for name, tc := range map[string]struct {
		store string
		key   []byte
	}{
		"evm misc key":                  {store: "evm", key: evmKey(0x09, 20)},
		"evm storage prefix, short key": {store: "evm", key: evmKey(0x03, 20)},
		"non-evm store":                 {store: "bank", key: evmKey(0x03, 52)},
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, ReadsAsAbsent(tc.store, tc.key, []byte{}))
			require.False(t, ReadsAsAbsent(tc.store, tc.key, make([]byte, 32)))
			require.False(t, ValuesEqual(tc.store, tc.key, nil, []byte{}))
			require.False(t, ValuesEqual(tc.store, tc.key, nil, make([]byte, 32)))
			require.True(t, ValuesEqual(tc.store, tc.key, []byte{}, []byte{}))
			require.True(t, ValuesEqual(tc.store, tc.key, nil, nil))
		})
	}
}

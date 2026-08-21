package query

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunOffsetPathUnfilteredSetsNextKeyWithTightPostPageBudget(t *testing.T) {
	kvStore := newTestKVStore(t)
	for i := 0; i < 3; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	req, err := normalizePageRequest(&PageRequest{Limit: 1, CountTotal: false})
	require.NoError(t, err)

	var count int
	res, err := runOffsetPathUnfiltered(kvStore, req, scanLimitParams{enforce: true, limit: 1}, func(_, _ []byte) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NotNil(t, res.NextKey)
	require.Equal(t, []byte("00000001"), res.NextKey)
}

func TestRunOffsetPathUnfilteredBreaksWithoutExtraNextAfterNextKey(t *testing.T) {
	kvStore := newTestKVStore(t)
	for i := 0; i < 4; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	req, err := normalizePageRequest(&PageRequest{Limit: 2, CountTotal: false})
	require.NoError(t, err)

	var seenKeys []string
	res, err := runOffsetPathUnfiltered(kvStore, req, scanLimitParams{enforce: false, limit: 0}, func(key, _ []byte) error {
		seenKeys = append(seenKeys, string(key))
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"00000000", "00000001"}, seenKeys)
	require.Equal(t, []byte("00000002"), res.NextKey)
}

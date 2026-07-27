package disktable

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// TestGetSubrange exercises the GetSubrange read path across every disk-table implementation, both before
// a flush (served from the in-memory unflushed data cache) and after (served from the keymap + a bounded
// segment read). It checks that a sub-range read returns exactly value[offset:offset+length], that it
// composes with secondary keys (which alias a sub-range of the value), that a zero-length read is valid,
// that an out-of-bounds range errors, and that a missing key reports not-found without an error.
func TestGetSubrange(t *testing.T) {
	t.Parallel()
	for _, tb := range tableBuilders {
		t.Run(tb.name, func(t *testing.T) {
			t.Parallel()
			rand := util.NewTestRandom()
			directory := t.TempDir()
			tableName := rand.String(8)
			table, err := tb.builder(time.Now, tableName, []string{directory})
			require.NoError(t, err)

			//                     0         1         2         3         4
			//                     0123456789012345678901234567890123456789012
			value := []byte("the quick brown fox jumps over the lazy dog")
			primary := []byte("primary")
			// A secondary aliasing the strict sub-range "brown fox", to prove GetSubrange composes with a
			// secondary key (its address already points at a sub-range of the value's bytes).
			sk := &types.SecondaryKey{Key: []byte("brown-fox"), Offset: 10, Length: 9}
			require.NoError(t, table.Put(primary, value, sk))

			valueLen := uint32(len(value))

			verify := func(stage string) {
				t.Helper()

				// The full range equals a plain Get.
				got, ok, err := table.GetSubrange(primary, 0, valueLen)
				require.NoError(t, err, stage)
				require.True(t, ok, stage)
				require.Equal(t, value, got, stage)

				// Assorted sub-ranges, including zero-length reads in the middle and at the very end.
				ranges := []struct{ off, length uint32 }{
					{0, 3},            // "the"
					{4, 5},            // "quick"
					{valueLen - 3, 3}, // "dog"
					{10, 0},           // zero-length in the middle
					{valueLen, 0},     // zero-length at the very end
				}
				for _, r := range ranges {
					got, ok, err := table.GetSubrange(primary, r.off, r.length)
					require.NoError(t, err, stage)
					require.True(t, ok, stage)
					require.NotNil(t, got, stage)
					require.Equal(t, value[r.off:r.off+r.length], got, stage)
				}

				// A sub-range read of a secondary key stays within the secondary's aliased region.
				got, ok, err = table.GetSubrange(sk.Key, 0, sk.Length)
				require.NoError(t, err, stage)
				require.True(t, ok, stage)
				require.Equal(t, value[sk.Offset:sk.Offset+sk.Length], got, stage)

				got, ok, err = table.GetSubrange(sk.Key, 6, 3) // "fox" within "brown fox"
				require.NoError(t, err, stage)
				require.True(t, ok, stage)
				require.Equal(t, value[sk.Offset+6:sk.Offset+6+3], got, stage)

				// A range that runs past the end of the value is an error.
				_, _, err = table.GetSubrange(primary, valueLen-1, 5)
				require.Error(t, err, stage)
				_, _, err = table.GetSubrange(primary, valueLen+1, 0)
				require.Error(t, err, stage)

				// A missing key reports not-found with no error.
				_, ok, err = table.GetSubrange([]byte("does-not-exist"), 0, 1)
				require.NoError(t, err, stage)
				require.False(t, ok, stage)
			}

			verify("before flush")
			require.NoError(t, table.Flush())
			verify("after flush")

			require.NoError(t, table.Drop())
		})
	}
}

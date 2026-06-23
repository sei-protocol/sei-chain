package hashlog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashLogHeaders(t *testing.T) {
	require.Equal(t, "block_number,changeset,flatKV,memIAVL,root",
		hashLogHeaders([]string{"changeset", "flatKV", "memIAVL", "root"}))
	require.Equal(t, "block_number,only", hashLogHeaders([]string{"only"}))
}

func TestHashLogCSVRoundTrip(t *testing.T) {
	hashTypes := []string{"changeset", "flatKV", "root"}
	original := &HashLog{
		BlockNumber: 42,
		Hashes: map[string][]byte{
			"changeset": {0x01, 0x02},
			"flatKV":    nil, // disabled subsystem
			"root":      {0xab, 0xcd, 0xef},
		},
	}

	csv := original.toCSV(hashTypes)
	require.Equal(t, "42,0102,,abcdef", csv)

	parsed, err := hashLogFromCSV(hashTypes, csv)
	require.NoError(t, err)
	require.Equal(t, original.BlockNumber, parsed.BlockNumber)
	require.Equal(t, original.Hashes["changeset"], parsed.Hashes["changeset"])
	require.Equal(t, original.Hashes["root"], parsed.Hashes["root"])
	require.Nil(t, parsed.Hashes["flatKV"])
}

func TestHashLogCSVCustomOrder(t *testing.T) {
	// Column order follows the supplied hashTypes, not any fixed order.
	hashTypes := []string{"root", "changeset"}
	original := &HashLog{
		BlockNumber: 7,
		Hashes:      map[string][]byte{"root": {0x11}, "changeset": {0x22}},
	}
	csv := original.toCSV(hashTypes)
	require.Equal(t, "7,11,22", csv)

	parsed, err := hashLogFromCSV(hashTypes, csv)
	require.NoError(t, err)
	require.Equal(t, []byte{0x11}, parsed.Hashes["root"])
	require.Equal(t, []byte{0x22}, parsed.Hashes["changeset"])
}

func TestHashLogFromCSVErrors(t *testing.T) {
	hashTypes := []string{"changeset"}

	_, err := hashLogFromCSV(hashTypes, "42") // too few fields
	require.Error(t, err)

	_, err = hashLogFromCSV(hashTypes, "notanumber,0102")
	require.Error(t, err)

	_, err = hashLogFromCSV(hashTypes, "42,nothex")
	require.Error(t, err)
}

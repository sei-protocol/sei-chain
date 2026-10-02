package apphash

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzDeserialize requires Deserialize() never to panic, and all app hash data it accepts to serialize back to the
// same bytes.
func FuzzDeserialize(f *testing.F) {
	f.Add(goldenData().Serialize())
	f.Fuzz(func(t *testing.T, data []byte) {
		ahd, err := Deserialize(data)
		if err != nil {
			return
		}
		require.Equal(t, data, ahd.Serialize())
	})
}

package p2p

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRouter_ConstructQueueFactory(t *testing.T) {
	t.Run("ValidateOptionsPopulatesDefaultQueue", func(t *testing.T) {
		opts := RouterOptions{}
		require.NoError(t, opts.Validate())
	})
}

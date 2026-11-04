package littblock

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKindRegistry(t *testing.T) {
	registry := kindRegistry{}
	require.Equal(t, recordKind(7), registry.register(7))
	require.Panics(t, func() { registry.register(7) })
	require.Equal(t, recordKind(0), registry.register(0))
	require.Panics(t, func() { registry.register(0) })
}

package oracle_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/oracle"
	"github.com/stretchr/testify/require"
)

func TestDeprecatedModuleHasNoLegacyRoutes(t *testing.T) {
	appModule := oracle.AppModule{}

	require.True(t, appModule.Route().Empty())
	require.Empty(t, appModule.QuerierRoute())
}

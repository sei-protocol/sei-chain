package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestConvertDecToStandard(t *testing.T) {
	actual := types.ConvertDecToStandard(types.Unit_NANO, sdk.MustNewDecFromStr("1"))

	require.Equal(t, sdk.MustNewDecFromStr("0.000000001"), actual)
}

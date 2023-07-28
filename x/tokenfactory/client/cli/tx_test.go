package cli

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestParseMetadata(t *testing.T) {
	cdc := codec.NewLegacyAmino()
	okJSON := testutil.WriteToNewTempFile(t, `
{
	"description": "Update token metadata",
	"denom_units": [
		{
			"denom": "doge1",
			"exponent": 6,
			"aliases": ["d", "o", "g"]
		},
		{
			"denom": "doge2",
			"exponent": 3,
			"aliases": ["d", "o", "g"]
		}
	],
	"base": "doge",
	"display": "DOGE",
	"name": "dogecoin",
	"symbol": "DOGE"
}
`)
	metadata, err := ParseMetadataJSON(cdc, okJSON.Name())
	require.NoError(t, err)

	require.Equal(t, banktypes.Metadata{
		Description: "Update token metadata",
		DenomUnits: []*banktypes.DenomUnit{{
			Denom:    "doge1",
			Exponent: 6,
			Aliases:  []string{"d", "o", "g"},
		}, {
			Denom:    "doge2",
			Exponent: 3,
			Aliases:  []string{"d", "o", "g"},
		}},
		Base:    "doge",
		Display: "DOGE",
		Name:    "dogecoin",
		Symbol:  "DOGE",
	}, metadata)
}

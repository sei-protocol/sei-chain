package query_test

import (
	"math/big"
	"reflect"
	"testing"

	pquery "github.com/sei-protocol/sei-chain/precompiles/query"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkquery "github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	"github.com/stretchr/testify/require"
)

type abiCoin struct {
	Amount *big.Int
	Denom  string
}

type helperFields struct {
	Amount   *big.Int
	Aliases  []string
	Enabled  bool
	Exponent uint8
	Name     string
}

func TestCoinsFromOutputAndPaginateCoins(t *testing.T) {
	coins, err := pquery.CoinsFromOutput([]interface{}{[]abiCoin{
		{Amount: big.NewInt(11), Denom: "usei"},
		{Amount: big.NewInt(7), Denom: "uatom"},
	}})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(
		sdk.NewCoin("uatom", sdk.NewInt(7)),
		sdk.NewCoin("usei", sdk.NewInt(11)),
	), coins)

	paged, pageRes, err := pquery.PaginateCoins(coins, &sdkquery.PageRequest{Limit: 1, CountTotal: true})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uatom", sdk.NewInt(7))), paged)
	require.Equal(t, []byte("usei"), pageRes.NextKey)
	require.Equal(t, uint64(2), pageRes.Total)
}

func TestFieldHelpers(t *testing.T) {
	fields := helperFields{
		Amount:   big.NewInt(42),
		Aliases:  []string{"microsei"},
		Enabled:  true,
		Exponent: 6,
		Name:     "usei",
	}
	value := reflect.ValueOf(&fields)

	name, err := pquery.FieldString(value, "Name")
	require.NoError(t, err)
	require.Equal(t, "usei", name)

	enabled, err := pquery.FieldBool(value, "Enabled")
	require.NoError(t, err)
	require.True(t, enabled)

	exponent, err := pquery.FieldUint32(value, "Exponent")
	require.NoError(t, err)
	require.Equal(t, uint32(6), exponent)

	amount, err := pquery.FieldBigInt(value, "Amount")
	require.NoError(t, err)
	require.Equal(t, big.NewInt(42), amount)

	aliases, err := pquery.FieldStringSlice(value, "Aliases")
	require.NoError(t, err)
	require.Equal(t, []string{"microsei"}, aliases)
}

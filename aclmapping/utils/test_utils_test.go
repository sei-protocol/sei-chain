package utils

import (
	"testing"

	tmdb "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestCacheTxContext(t *testing.T) {
	// Create a new Context with MultiStore
	db := tmdb.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ctx := sdk.NewContext(ms, tmproto.Header{}, false, nil)

	// Call the CacheTxContext method
	newCtx, newMs := CacheTxContext(ctx)

	// Verify that newCtx has the same MultiStore as newMs
	require.Equal(t, newMs, newCtx.MultiStore())

	// Verify that the original Context was not modified
	require.Equal(t, ms, ctx.MultiStore())
}

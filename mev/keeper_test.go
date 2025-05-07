package mev_test

import (
	types "github.com/SiloMEV/silo-mev-protobuf-go/mev/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/app"
)

func setupKeeper(t *testing.T) (*app.App, tmproto.Header) {
	app := app.Setup(false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	return app, ctx.BlockHeader()
}

func TestKeeper_SubmitAndQueryBundles(t *testing.T) {
	app, _ := setupKeeper(t)

	height := int64(100)

	// Submit a bundle
	bundle := &types.Bundle{
		Transactions: [][]byte{[]byte("tx1"), []byte("tx2")},
		BlockHeight:  uint64(height),
	}

	res := app.MevKeeper.SetBundles(height, []*types.Bundle{bundle})
	require.True(t, res)

	// Query bundles
	pending := app.MevKeeper.PendingBundles(height)
	require.Len(t, pending, 1)
	require.Equal(t, bundle.Transactions[0], pending[0].Transactions[0])
}

func TestKeeper_BundlesSetting(t *testing.T) {
	app, _ := setupKeeper(t)

	height := int64(100)

	// Submit a bundle
	bundle := &types.Bundle{
		Transactions: [][]byte{[]byte("tx1"), []byte("tx2")},
		BlockHeight:  uint64(height),
	}

	res := app.MevKeeper.SetBundles(height, []*types.Bundle{bundle})
	require.True(t, res)

	// Query bundles
	pending := app.MevKeeper.PendingBundles(height)
	require.Len(t, pending, 1)
	require.Equal(t, bundle.Transactions[0], pending[0].Transactions[0])

	//set new bundles for a height
	newBundleTx := []byte("tx3")
	bundle.Transactions = [][]byte{newBundleTx}
	res = app.MevKeeper.SetBundles(height, []*types.Bundle{bundle})
	require.True(t, res)

	pending = app.MevKeeper.PendingBundles(height)
	require.Equal(t, 1, len(pending))
	require.Equal(t, bundle.Transactions[0], pending[0].Transactions[0])
}

func TestKeeper_BundlesPurging(t *testing.T) {
	app, _ := setupKeeper(t)

	height := int64(100)

	// empty keeper drops bundles just fine
	app.MevKeeper.DropBundlesAtAndBelow(height)

	app.MevKeeper.PendingBundles(height)
	require.Equal(t, 0, len(app.MevKeeper.PendingBundles(height)))

	// Submit a bundle
	bundle := &types.Bundle{
		Transactions: [][]byte{[]byte("tx1"), []byte("tx2")},
		BlockHeight:  uint64(height),
	}

	res := app.MevKeeper.SetBundles(height, []*types.Bundle{bundle})
	require.True(t, res)

	// Query bundles
	pending := app.MevKeeper.PendingBundles(height)
	require.Len(t, pending, 1)

	// Drop and check if gone
	app.MevKeeper.DropBundlesAtAndBelow(height)
	require.Equal(t, 0, len(app.MevKeeper.PendingBundles(height)))

	// add series of bundles
	for height := int64(100); height < 1000; height += 23 {
		bundle := &types.Bundle{
			Transactions: [][]byte{[]byte("tx1"), []byte("tx2")},
			BlockHeight:  uint64(height),
		}

		res := app.MevKeeper.SetBundles(height, []*types.Bundle{bundle})
		require.True(t, res)
	}

	// check series of bundles
	for height := int64(100); height < 1000; height += 23 {
		pending := app.MevKeeper.PendingBundles(height)
		require.Len(t, pending, 1)
	}

	app.MevKeeper.DropBundlesAtAndBelow(500)

	// check everything below 500 is gone, but above is up
	for height := int64(100); height < 500; height += 23 {
		pending := app.MevKeeper.PendingBundles(height)
		require.Len(t, pending, 0)
	}
	// first bundle above 500 is at 514
	for height := int64(514); height < 1000; height += 23 {
		pending := app.MevKeeper.PendingBundles(height)
		require.Len(t, pending, 1)
	}

	app.MevKeeper.DropBundlesAtAndBelow(1000)

	// now everything is gone
	for height := int64(100); height < 1000; height += 23 {
		pending := app.MevKeeper.PendingBundles(height)
		require.Len(t, pending, 0)
	}
}

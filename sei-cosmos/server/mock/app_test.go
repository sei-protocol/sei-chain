package mock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/types"
)

// TestInitApp makes sure we can initialize this thing without an error
func TestInitApp(t *testing.T) {
	// set up an app
	app, closer, err := SetupApp()

	// closer may need to be run, even when error in later stage
	if closer != nil {
		defer closer()
	}
	require.NoError(t, err)

	// initialize it future-way
	appState, err := AppGenState(nil, types.GenesisDoc{}, nil)
	require.NoError(t, err)

	//TODO test validators in the init chain?
	req := abci.RequestInitChain{
		AppStateBytes: appState,
	}
	app.InitChain(context.Background(), &req)
	app.Commit(context.Background())

	// make sure we can query these values
	query := abci.RequestQuery{
		Path: "/store/main/key",
		Data: []byte("foo"),
	}
	qres, _ := app.Query(context.Background(), &query)
	require.Equal(t, uint32(0), qres.Code, qres.Log)
	require.Equal(t, []byte("bar"), qres.Value)
}

// TextDeliverTx ensures we can write a tx
func TestDeliverTx(t *testing.T) {
	// set up an app
	app, closer, err := SetupApp()
	// closer may need to be run, even when error in later stage
	if closer != nil {
		defer closer()
	}
	require.NoError(t, err)

	key := "my-special-key"
	value := "top-secret-data!!"
	tx := NewTx(key, value)
	txBytes := tx.GetSignBytes()

	goCtx := context.Background()
	appState, err := AppGenState(nil, types.GenesisDoc{}, nil)
	require.NoError(t, err)

	//TODO test validators in the init chain?
	req := abci.RequestInitChain{
		AppStateBytes: appState,
	}
	app.InitChain(goCtx, &req)
	app.FinalizeBlock(goCtx, &abci.RequestFinalizeBlock{
		Hash:   []byte("apphash"),
		Height: 1,
		Txs:    [][]byte{txBytes},
	})
	app.Commit(goCtx)

	// make sure we can query these values
	query := abci.RequestQuery{
		Path: "/store/main/key",
		Data: []byte(key),
	}
	qres, _ := app.Query(goCtx, &query)
	require.Equal(t, uint32(0), qres.Code, qres.Log)
	require.Equal(t, []byte(value), qres.Value)
}

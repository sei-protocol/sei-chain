package baseapp

import (
	"context"
	"fmt"
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

func anteHandler(capKey sdk.StoreKey, storeKey []byte) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		store := ctx.KVStore(capKey)
		txTest := tx.(txTest)

		if txTest.FailOnAnte {
			return ctx, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "ante handler failure")
		}

		val := getIntFromStore(store, storeKey)
		setIntOnStore(store, storeKey, val+1)

		ctx.EventManager().EmitEvents(
			counterEvent("ante-val", val+1),
		)

		return ctx, nil
	}
}

func handlerKVStore(capKey sdk.StoreKey) sdk.Handler {
	return func(ctx sdk.Context, msg sdk.Msg) (*sdk.Result, error) {
		ctx = ctx.WithEventManager(sdk.NewEventManager())
		res := &sdk.Result{}

		// Extract the unique ID from the message (assuming you have added this)
		txIndex := ctx.TxIndex()

		// Use the unique ID to get a specific key for this transaction
		sharedKey := []byte(fmt.Sprintf("shared"))
		txKey := []byte(fmt.Sprintf("tx-%d", txIndex))

		// Similar steps as before: Get the store, retrieve a value, increment it, store back, emit an event
		// Get the store
		store := ctx.KVStore(capKey)

		// increment per-tx key (no conflict)
		val := getIntFromStore(store, txKey)
		setIntOnStore(store, txKey, val+1)

		// increment shared key
		sharedVal := getIntFromStore(store, sharedKey)
		setIntOnStore(store, sharedKey, sharedVal+1)

		// Emit an event with the incremented value and the unique ID
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(sdk.EventTypeMessage,
				sdk.NewAttribute("shared-val", fmt.Sprintf("%d", sharedVal+1)),
				sdk.NewAttribute("tx-val", fmt.Sprintf("%d", val+1)),
				sdk.NewAttribute("tx-id", fmt.Sprintf("%d", txIndex)),
			),
		)

		res.Events = ctx.EventManager().Events().ToABCIEvents()
		return res, nil
	}
}

func requireAttribute(t *testing.T, evts []abci.Event, name string, val string) {
	for _, evt := range evts {
		for _, att := range evt.Attributes {
			if string(att.Key) == name {
				require.Equal(t, val, string(att.Value))
				return
			}
		}
	}
	require.Fail(t, fmt.Sprintf("attribute %s not found via value %s", name, val))
}

func TestDeliverTxBatch(t *testing.T) {
	// test increments in the ante
	anteKey := []byte("ante-key")

	anteOpt := func(bapp *BaseApp) {
		bapp.SetAnteHandler(anteHandler(capKey1, anteKey))
	}

	// test increments in the handler
	routerOpt := func(bapp *BaseApp) {
		r := sdk.NewRoute(routeMsgCounter, handlerKVStore(capKey1))
		bapp.Router().AddRoute(r)
	}

	app := setupBaseApp(t, anteOpt, routerOpt)
	app.InitChain(context.Background(), &abci.RequestInitChain{})

	// Create same codec used in txDecoder
	codec := codec.NewLegacyAmino()
	registerTestCodec(codec)

	nBlocks := 3
	txPerHeight := 5

	for blockN := 0; blockN < nBlocks; blockN++ {
		header := tmproto.Header{Height: int64(blockN) + 1}
		app.setDeliverState(header)

		var requests []*sdk.DeliverTxEntry
		for i := 0; i < txPerHeight; i++ {
			counter := int64(blockN*txPerHeight + i)
			tx := newTxCounter(counter, counter)

			txBytes, err := codec.Marshal(tx)
			require.NoError(t, err)
			requests = append(requests, &sdk.DeliverTxEntry{
				Request:       abci.RequestDeliverTxV2{Tx: txBytes},
				SdkTx:         *tx,
				AbsoluteIndex: i,
			})
		}

		responses := app.DeliverTxBatch(app.deliverState.ctx, sdk.DeliverTxBatchRequest{TxEntries: requests})
		require.Len(t, responses.Results, txPerHeight)

		for idx, deliverTxRes := range responses.Results {
			res := deliverTxRes.Response
			require.Equal(t, abci.CodeTypeOK, res.Code)
			requireAttribute(t, res.Events, "tx-id", fmt.Sprintf("%d", idx))
			requireAttribute(t, res.Events, "tx-val", fmt.Sprintf("%d", blockN+1))
			requireAttribute(t, res.Events, "shared-val", fmt.Sprintf("%d", blockN*txPerHeight+idx+1))
		}

		app.EndBlock(app.deliverState.ctx, abci.RequestEndBlock{})
		app.SetDeliverStateToCommit()
		app.Commit(context.Background())
	}
}

func TestDeliverTxBatchEmpty(t *testing.T) {
	// test increments in the ante
	anteKey := []byte("ante-key")

	anteOpt := func(bapp *BaseApp) {
		bapp.SetAnteHandler(anteHandler(capKey1, anteKey))
	}

	// test increments in the handler
	routerOpt := func(bapp *BaseApp) {
		r := sdk.NewRoute(routeMsgCounter, handlerKVStore(capKey1))
		bapp.Router().AddRoute(r)
	}

	app := setupBaseApp(t, anteOpt, routerOpt)
	app.InitChain(context.Background(), &abci.RequestInitChain{})

	// Create same codec used in txDecoder
	codec := codec.NewLegacyAmino()
	registerTestCodec(codec)

	nBlocks := 3
	for blockN := 0; blockN < nBlocks; blockN++ {
		header := tmproto.Header{Height: int64(blockN) + 1}
		app.setDeliverState(header)

		var requests []*sdk.DeliverTxEntry
		responses := app.DeliverTxBatch(app.deliverState.ctx, sdk.DeliverTxBatchRequest{TxEntries: requests})
		require.Len(t, responses.Results, 0)

		app.EndBlock(app.deliverState.ctx, abci.RequestEndBlock{})
		app.SetDeliverStateToCommit()
		app.Commit(context.Background())
	}
}

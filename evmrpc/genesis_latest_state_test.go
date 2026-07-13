package evmrpc_test

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/mock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmstate "github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type freshChainClient struct {
	mock.Client
}

func (*freshChainClient) EvmNextPendingNonce(common.Address) uint64 {
	return 0
}

func (*freshChainClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) {
	return nil, false
}

func (*freshChainClient) EvmProxy(common.Address) utils.Option[*url.URL] {
	return utils.None[*url.URL]()
}

func (*freshChainClient) Status(context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{
		SyncInfo: coretypes.SyncInfo{
			LatestBlockHeight:   0,
			EarliestBlockHeight: 1,
		},
	}, nil
}

func (*freshChainClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return &coretypes.ResultGenesis{Genesis: &tmtypes.GenesisDoc{InitialHeight: 1}}, nil
}

func (*freshChainClient) Block(context.Context, *int64) (*coretypes.ResultBlock, error) {
	return nil, coretypes.ErrZeroOrNegativeHeight
}

func (*freshChainClient) BlockByHash(context.Context, bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return &coretypes.ResultBlock{Block: &tmtypes.Block{Header: tmtypes.Header{Height: 0}}}, nil
}

func latestLikeTags() []rpc.BlockNumber {
	return []rpc.BlockNumber{
		rpc.LatestBlockNumber,
		rpc.SafeBlockNumber,
		rpc.FinalizedBlockNumber,
		rpc.PendingBlockNumber,
	}
}

func TestStateAPILatestLikeTagsUseGenesisCheckStateBeforeFirstCommit(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	checkCtx := testApp.GetCheckCtx()
	_, address := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("key"))
	value := common.BytesToHash([]byte("value"))
	code := []byte{0xaa, 0xbb, 0xcc}
	amount := sdk.NewInt(1234)

	coins := sdk.NewCoins(sdk.NewCoin(testApp.EvmKeeper.GetBaseDenom(checkCtx), amount))
	require.NoError(t, testApp.EvmKeeper.BankKeeper().MintCoins(checkCtx, evmtypes.ModuleName, coins))
	require.NoError(t, testApp.EvmKeeper.BankKeeper().SendCoinsFromModuleToAccount(checkCtx, evmtypes.ModuleName, sdk.AccAddress(address[:]), coins))
	testApp.EvmKeeper.SetCode(checkCtx, address, code)
	testApp.EvmKeeper.SetState(checkCtx, address, key, value)

	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return testApp.GetCheckCtx()
		}
		queryCtx, err := testApp.CreateQueryContext(height, false)
		require.NoError(t, err)
		return queryCtx
	}
	tmClient := &freshChainClient{}
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
	api := evmrpc.NewStateAPI(tmClient, &testApp.EvmKeeper, ctxProvider, evmrpc.ConnectionTypeHTTP, watermarks)
	expectedBalance := evmstate.NewDBImpl(testApp.GetCheckCtx(), &testApp.EvmKeeper, true).GetBalance(address).ToBig().String()

	for _, tag := range latestLikeTags() {
		blockRef := rpc.BlockNumberOrHashWithNumber(tag)

		balance, err := api.GetBalance(t.Context(), address, blockRef)
		require.NoError(t, err)
		require.Equal(t, expectedBalance, (*balance).ToInt().String())

		gotCode, err := api.GetCode(t.Context(), address, blockRef)
		require.NoError(t, err)
		require.Equal(t, code, []byte(gotCode))

		storage, err := api.GetStorageAt(t.Context(), address, key.Hex(), blockRef)
		require.NoError(t, err)
		require.Equal(t, value[:], []byte(storage))
	}
}

func TestSimulationBackendLatestLikeTagsUseGenesisCheckStateBeforeFirstCommit(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	checkCtx := testApp.GetCheckCtx()
	_, address := testkeeper.MockAddressPair()
	code := []byte{0xde, 0xad, 0xbe, 0xef}
	amount := sdk.NewInt(4321)

	coins := sdk.NewCoins(sdk.NewCoin(testApp.EvmKeeper.GetBaseDenom(checkCtx), amount))
	require.NoError(t, testApp.EvmKeeper.BankKeeper().MintCoins(checkCtx, evmtypes.ModuleName, coins))
	require.NoError(t, testApp.EvmKeeper.BankKeeper().SendCoinsFromModuleToAccount(checkCtx, evmtypes.ModuleName, sdk.AccAddress(address[:]), coins))
	testApp.EvmKeeper.SetCode(checkCtx, address, code)

	tmClient := &freshChainClient{}
	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return testApp.GetCheckCtx()
		}
		queryCtx, err := testApp.CreateQueryContext(height, false)
		require.NoError(t, err)
		return queryCtx
	}
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
	backend := evmrpc.NewBackend(
		ctxProvider,
		&testApp.EvmKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) client.TxConfig { return app.MakeEncodingConfig().TxConfig },
		tmClient,
		&evmrpc.SimulateConfig{GasCap: 1_000_000, EVMTimeout: time.Second},
		testApp.BaseApp,
		testApp.TracerAnteHandler,
		evmrpc.NewBlockCache(16),
		&sync.Mutex{},
		watermarks,
	)
	expectedBalance := evmstate.NewDBImpl(testApp.GetCheckCtx(), &testApp.EvmKeeper, true).GetBalance(address).ToBig().String()

	for _, tag := range latestLikeTags() {
		statedb, header, err := backend.StateAndHeaderByNumberOrHash(context.Background(), rpc.BlockNumberOrHashWithNumber(tag))
		require.NoError(t, err)
		require.Equal(t, checkCtx.BlockHeight(), header.Number.Int64())
		require.Equal(t, code, statedb.GetCode(address))

		db := evmstate.GetDBImpl(statedb)
		require.Equal(t, expectedBalance, db.GetBalance(address).ToBig().String())
	}
}

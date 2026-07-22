package evmrpc_test

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

const (
	rangeCapTestHeight = int64(42)
	rangeCapBlockHash  = "cacacacacacacacacacacacacacacacacacacacacacacacacacacacacacaca"
)

// rangeCapReceiptStore simulates a litt-style range-query backend: FilterLogs
// returns preconfigured candidate logs (optionally over-counting tag-index
// matches) while GetReceipt serves canonical receipts for normalization.
type rangeCapReceiptStore struct {
	mu         sync.Mutex
	receipts   map[common.Hash]*types.Receipt
	candidates []*ethtypes.Log
	latest     int64
	earliest   int64
}

func newRangeCapReceiptStore() *rangeCapReceiptStore {
	return &rangeCapReceiptStore{
		receipts: make(map[common.Hash]*types.Receipt),
		latest:   rangeCapTestHeight,
		earliest: 1,
	}
}

func (s *rangeCapReceiptStore) LatestVersion() int64   { return s.latest }
func (s *rangeCapReceiptStore) EarliestVersion() int64 { return s.earliest }

func (s *rangeCapReceiptStore) SetLatestVersion(version int64) error {
	s.latest = version
	return nil
}

func (s *rangeCapReceiptStore) SetEarliestVersion(version int64) error {
	s.earliest = version
	return nil
}

func (s *rangeCapReceiptStore) GetReceipt(_ sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rcpt, ok := s.receipts[txHash]
	if !ok {
		return nil, receipt.ErrNotFound
	}
	return rcpt, nil
}

func (s *rangeCapReceiptStore) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	return s.GetReceipt(ctx, txHash)
}

func (s *rangeCapReceiptStore) SetReceipts(_ sdk.Context, records []receipt.ReceiptRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range records {
		s.receipts[record.TxHash] = record.Receipt
	}
	return nil
}

func (s *rangeCapReceiptStore) FilterLogs(_ sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria, budget *receipt.LogBudget) ([]*ethtypes.Log, error) {
	s.mu.Lock()
	candidates := append([]*ethtypes.Log(nil), s.candidates...)
	s.mu.Unlock()

	out := make([]*ethtypes.Log, 0, len(candidates))
	for _, lg := range candidates {
		if lg.BlockNumber < fromBlock || lg.BlockNumber > toBlock {
			continue
		}
		if len(crit.Addresses) > 0 || len(crit.Topics) > 0 {
			if !evmrpc.MatchesCriteriaForTest(lg, crit) {
				continue
			}
		}
		if budget != nil {
			if err := budget.Reserve(lg); err != nil {
				return nil, err
			}
		}
		out = append(out, lg)
	}
	return out, nil
}

func (s *rangeCapReceiptStore) Close() error { return nil }

func (s *rangeCapReceiptStore) setCandidates(candidates []*ethtypes.Log) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates = candidates
}

type rangeCapTMClient struct {
	client.LocalClient
	block *coretypes.ResultBlock
}

func (c *rangeCapTMClient) EvmNextPendingNonce(common.Address) uint64 { return 0 }

func (c *rangeCapTMClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) { return nil, false }

func (c *rangeCapTMClient) EvmProxy(common.Address) utils.Option[*url.URL] {
	return utils.None[*url.URL]()
}

func (c *rangeCapTMClient) Block(_ context.Context, _ *int64) (*coretypes.ResultBlock, error) {
	return c.block, nil
}

func (c *rangeCapTMClient) BlockResults(_ context.Context, _ *int64) (*coretypes.ResultBlockResults, error) {
	txResults := make([]*abci.ExecTxResult, len(c.block.Block.Txs))
	for i := range txResults {
		txResults[i] = &abci.ExecTxResult{}
	}
	return &coretypes.ResultBlockResults{
		Height:     c.block.Block.Height,
		TxsResults: txResults,
	}, nil
}

func (c *rangeCapTMClient) Status(_ context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{
		SyncInfo: coretypes.SyncInfo{
			LatestBlockHeight:   c.block.Block.Height,
			EarliestBlockHeight: 1,
		},
	}, nil
}

type rangeCapFixture struct {
	fetcher  *evmrpc.LogFetcher
	keeper   *evmkeeper.Keeper
	sdkCtx   sdk.Context
	ctx      context.Context
	match    common.Address
	ethTx    *ethtypes.Transaction
	blockNum uint64
}

func setupRangeCapFixture(t *testing.T, receiptLogCount int, candidates []*ethtypes.Log, filterCfg evmrpc.FilterConfigTest) *rangeCapFixture {
	t.Helper()

	testApp := app.Setup(t, false, false, false)
	txConfig := testApp.GetTxConfig()
	sdkCtx := testApp.GetContextForDeliverTx([]byte{}).
		WithBlockHeight(rangeCapTestHeight).
		WithBlockTime(time.Unix(1700000000, 0)).
		WithClosestUpgradeName("v6.0.0")

	_, toAddr := testkeeper.MockAddressPair()
	gp := sdk.NewInt(1)
	amt := sdk.NewInt(1)
	builder := txConfig.NewTxBuilder()
	msg, err := types.NewMsgEVMTransaction(&ethtx.LegacyTx{
		Nonce:    0,
		GasPrice: &gp,
		GasLimit: 21000,
		To:       toAddr.Hex(),
		Amount:   &amt,
	})
	require.NoError(t, err)
	require.NoError(t, builder.SetMsgs(msg))
	txBz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)

	ethTx, _ := msg.AsTransaction()
	require.NotNil(t, ethTx)

	matchAddr := common.HexToAddress(LogCapAddr)
	topic := common.HexToHash(LogCapBlockHash)
	receiptLogs, ethLogs := buildRangeCapLogs(receiptLogCount, matchAddr, topic)
	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: ethLogs})

	store := newRangeCapReceiptStore()
	store.setCandidates(candidates)
	testApp.EvmKeeper.SetReceiptStoreForTesting(store)
	require.NoError(t, testApp.EvmKeeper.MockReceipt(sdkCtx, ethTx.Hash(), &types.Receipt{
		BlockNumber:       uint64(rangeCapTestHeight),
		TransactionIndex:  0,
		TxHashHex:         ethTx.Hash().Hex(),
		LogsBloom:         bloom[:],
		Logs:              receiptLogs,
		GasUsed:           21000,
		EffectiveGasPrice: 100,
	}))

	blockHashBytes, err := hex.DecodeString(rangeCapBlockHash)
	require.NoError(t, err)
	block := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: bytes.HexBytes(blockHashBytes)},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{
				ChainID: "test",
				Height:  rangeCapTestHeight,
				Time:    time.Unix(1700000000, 0),
			},
			Data:       tmtypes.Data{Txs: []tmtypes.Tx{txBz}},
			LastCommit: &tmtypes.Commit{Height: rangeCapTestHeight - 1},
		},
	}

	tmClient := &rangeCapTMClient{block: block}
	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return sdkCtx
		}
		return sdkCtx.WithBlockHeight(height)
	}
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, store)
	fetcher := evmrpc.NewLogFetcherForTest(evmrpc.LogFetcherTestDeps{
		TmClient:         tmClient,
		K:                &testApp.EvmKeeper,
		TxConfigProvider: func(int64) client.TxConfig { return txConfig },
		CtxProvider:      ctxProvider,
		FilterConfig:     evmrpc.NewFilterConfigForTest(filterCfg),
		Watermarks:       watermarks,
	})

	return &rangeCapFixture{
		fetcher:  fetcher,
		keeper:   &testApp.EvmKeeper,
		sdkCtx:   sdkCtx,
		ctx:      context.Background(),
		match:    matchAddr,
		ethTx:    ethTx,
		blockNum: uint64(rangeCapTestHeight),
	}
}

func buildRangeCapLogs(count int, addr common.Address, topic common.Hash) ([]*types.Log, []*ethtypes.Log) {
	receiptLogs := make([]*types.Log, 0, count)
	ethLogs := make([]*ethtypes.Log, 0, count)
	for i := 0; i < count; i++ {
		receiptLogs = append(receiptLogs, &types.Log{
			Address: addr.Hex(),
			Topics:  []string{topic.Hex()},
		})
		ethLogs = append(ethLogs, &ethtypes.Log{
			Address: addr,
			Topics:  []common.Hash{topic},
		})
	}
	return receiptLogs, ethLogs
}

func buildRangeCapCandidates(count int, blockNum uint64, addr common.Address, topic common.Hash, data []byte) []*ethtypes.Log {
	out := make([]*ethtypes.Log, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, &ethtypes.Log{
			Address:     addr,
			Topics:      []common.Hash{topic},
			Data:        data,
			BlockNumber: blockNum,
		})
	}
	return out
}

func rangeCapCriteria(match common.Address) filters.FilterCriteria {
	return filters.FilterCriteria{Addresses: []common.Address{match}}
}

func rangeCapBlockCriteria(match common.Address) filters.FilterCriteria {
	return filters.FilterCriteria{
		FromBlock: big.NewInt(rangeCapTestHeight),
		ToBlock:   big.NewInt(rangeCapTestHeight),
		Addresses: []common.Address{match},
	}
}

func TestTryFilterLogsRangeCountCap(t *testing.T) {
	t.Parallel()

	match := common.HexToAddress(LogCapAddr)
	topic := common.HexToHash(LogCapBlockHash)
	candidates := buildRangeCapCandidates(20, uint64(rangeCapTestHeight), match, topic, nil)
	fixture := setupRangeCapFixture(t, 11, candidates, evmrpc.FilterConfigTest{
		MaxLog:   10,
		MaxBlock: evmrpc.DefaultMaxBlockRange,
	})

	logs, err := fixture.fetcher.TryFilterLogsRangeForTest(
		fixture.ctx,
		fixture.blockNum,
		fixture.blockNum,
		rangeCapCriteria(fixture.match),
		10,
	)
	require.Error(t, err)
	require.Nil(t, logs)
	require.True(t, errors.Is(err, receipt.ErrTooManyLogs))
}

func TestTryFilterLogsRangeSyntheticFalsePositiveGuard(t *testing.T) {
	t.Parallel()

	match := common.HexToAddress(LogCapAddr)
	topic := common.HexToHash(LogCapBlockHash)
	candidates := buildRangeCapCandidates(50, uint64(rangeCapTestHeight), match, topic, nil)
	fixture := setupRangeCapFixture(t, 5, candidates, evmrpc.FilterConfigTest{
		MaxLog:   10,
		MaxBlock: evmrpc.DefaultMaxBlockRange,
	})

	logs, err := fixture.fetcher.TryFilterLogsRangeForTest(
		fixture.ctx,
		fixture.blockNum,
		fixture.blockNum,
		rangeCapCriteria(fixture.match),
		10,
	)
	require.NoError(t, err)
	require.Len(t, logs, 5)
	for i, lg := range logs {
		require.Equal(t, fixture.blockNum, lg.BlockNumber)
		require.Equal(t, uint(i), lg.Index)
		require.Equal(t, uint(0), lg.TxIndex)
		require.NotZero(t, lg.BlockHash)
	}
}

func TestTryFilterLogsRangeByteCapAtStore(t *testing.T) {
	t.Parallel()

	match := common.HexToAddress(LogCapAddr)
	topic := common.HexToHash(LogCapBlockHash)
	huge := make([]byte, 128<<10)
	single := buildRangeCapCandidates(1, uint64(rangeCapTestHeight), match, topic, huge)
	maxBytes := receipt.EstimateLogHeapBytes(single[0]) - 1
	fixture := setupRangeCapFixture(t, 1, single, evmrpc.FilterConfigTest{
		MaxLog:      1000,
		MaxLogBytes: maxBytes,
		MaxBlock:    evmrpc.DefaultMaxBlockRange,
	})

	logs, err := fixture.fetcher.TryFilterLogsRangeForTest(
		fixture.ctx,
		fixture.blockNum,
		fixture.blockNum,
		rangeCapCriteria(fixture.match),
		1000,
	)
	require.Error(t, err)
	require.Nil(t, logs)
	require.True(t, errors.Is(err, receipt.ErrTooManyLogBytes))
}

func TestTryFilterLogsRangeByteCapAtNormalize(t *testing.T) {
	t.Parallel()

	match := common.HexToAddress(LogCapAddr)
	topic := common.HexToHash(LogCapBlockHash)
	huge := make([]byte, 128<<10)
	receiptLogs, ethLogs := buildRangeCapLogs(3, match, topic)
	for i := range receiptLogs {
		receiptLogs[i].Data = huge
		ethLogs[i].Data = huge
	}
	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: ethLogs})
	maxBytes := receipt.EstimateLogHeapBytes(ethLogs[0])*2 + receipt.EstimateLogHeapBytes(ethLogs[1]) - 1

	candidates := buildRangeCapCandidates(1, uint64(rangeCapTestHeight), match, topic, nil)
	fixture := setupRangeCapFixture(t, 0, candidates, evmrpc.FilterConfigTest{
		MaxLog:      1000,
		MaxLogBytes: maxBytes,
		MaxBlock:    evmrpc.DefaultMaxBlockRange,
	})
	require.NoError(t, fixture.keeper.MockReceipt(
		fixture.sdkCtx,
		fixture.ethTx.Hash(),
		&types.Receipt{
			BlockNumber:       uint64(rangeCapTestHeight),
			TransactionIndex:  0,
			TxHashHex:         fixture.ethTx.Hash().Hex(),
			LogsBloom:         bloom[:],
			Logs:              receiptLogs,
			GasUsed:           21000,
			EffectiveGasPrice: 100,
		},
	))

	logs, err := fixture.fetcher.TryFilterLogsRangeForTest(
		fixture.ctx,
		fixture.blockNum,
		fixture.blockNum,
		rangeCapCriteria(fixture.match),
		1000,
	)
	require.Error(t, err)
	require.Nil(t, logs)
	require.True(t, errors.Is(err, receipt.ErrTooManyLogBytes))
}

func TestGetLogsByFiltersRangePathUsesNormalizeBudget(t *testing.T) {
	t.Parallel()

	match := common.HexToAddress(LogCapAddr)
	topic := common.HexToHash(LogCapBlockHash)
	candidates := buildRangeCapCandidates(30, uint64(rangeCapTestHeight), match, topic, nil)
	fixture := setupRangeCapFixture(t, 5, candidates, evmrpc.FilterConfigTest{
		MaxLog:   10,
		MaxBlock: evmrpc.DefaultMaxBlockRange,
	})

	logs, _, err := fixture.fetcher.GetLogsByFilters(fixture.ctx, rangeCapBlockCriteria(match), 0)
	require.NoError(t, err)
	require.Len(t, logs, 5)
}

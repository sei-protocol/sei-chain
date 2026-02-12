package evmrpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/bitutil"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	EthNamespace  = "eth"
	SeiNamespace  = "sei"
	Sei2Namespace = "sei2"
)

type BlockAPI struct {
	tmClient             rpcclient.Client
	keeper               *keeper.Keeper
	ctxProvider          func(int64) sdk.Context
	txConfigProvider     func(int64) client.TxConfig
	connectionType       ConnectionType
	namespace            string
	includeShellReceipts bool
	includeBankTransfers bool
	watermarks           *WatermarkManager
	globalBlockCache     BlockCache
	cacheCreationMutex   *sync.Mutex
}

type SeiBlockAPI struct {
	*BlockAPI
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error)
}

func NewBlockAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, connectionType ConnectionType, watermarks *WatermarkManager, globalBlockCache BlockCache, cacheCreationMutex *sync.Mutex) *BlockAPI {
	return &BlockAPI{
		tmClient:             tmClient,
		keeper:               k,
		ctxProvider:          ctxProvider,
		txConfigProvider:     txConfigProvider,
		connectionType:       connectionType,
		includeShellReceipts: false,
		includeBankTransfers: false,
		namespace:            EthNamespace,
		watermarks:           watermarks,
		globalBlockCache:     globalBlockCache,
		cacheCreationMutex:   cacheCreationMutex,
	}
}

func NewSeiBlockAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	connectionType ConnectionType,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
	watermarks *WatermarkManager,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
) *SeiBlockAPI {
	blockAPI := &BlockAPI{
		tmClient:             tmClient,
		keeper:               k,
		ctxProvider:          ctxProvider,
		txConfigProvider:     txConfigProvider,
		connectionType:       connectionType,
		includeShellReceipts: true,
		includeBankTransfers: false,
		namespace:            SeiNamespace,
		watermarks:           watermarks,
		globalBlockCache:     globalBlockCache,
		cacheCreationMutex:   cacheCreationMutex,
	}
	return &SeiBlockAPI{
		BlockAPI:  blockAPI,
		isPanicTx: isPanicTx,
	}
}

func NewSei2BlockAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	connectionType ConnectionType,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
	watermarks *WatermarkManager,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
) *SeiBlockAPI {
	blockAPI := NewSeiBlockAPI(tmClient, k, ctxProvider, txConfigProvider, connectionType, isPanicTx, watermarks, globalBlockCache, cacheCreationMutex)
	blockAPI.namespace = Sei2Namespace
	blockAPI.includeBankTransfers = true
	return blockAPI
}

func (a *SeiBlockAPI) GetBlockByNumberExcludeTraceFail(ctx context.Context, number rpc.BlockNumber, fullTx bool) (result map[string]interface{}, returnErr error) {
	// exclude synthetic txs
	return a.getBlockByNumber(ctx, number, fullTx, false, a.isPanicTx)
}

func (a *SeiBlockAPI) GetBlockByHashExcludeTraceFail(ctx context.Context, blockHash common.Hash, fullTx bool) (result map[string]interface{}, returnErr error) {
	// exclude synthetic txs
	return a.getBlockByHash(ctx, blockHash, fullTx, false, a.isPanicTx)
}

func (a *BlockAPI) GetBlockTransactionCountByNumber(ctx context.Context, number rpc.BlockNumber) (result *hexutil.Uint, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError(fmt.Sprintf("%s_getBlockTransactionCountByNumber", a.namespace), a.connectionType, startTime, returnErr)
	numberPtr, err := getBlockNumber(ctx, a.tmClient, number)
	if err != nil {
		return nil, err
	}
	block, err := blockByNumberRespectingWatermarks(ctx, a.tmClient, a.watermarks, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block.Block.Txs, block.Block.Height), nil
}

func (a *BlockAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) (result *hexutil.Uint, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError(fmt.Sprintf("%s_getBlockTransactionCountByHash", a.namespace), a.connectionType, startTime, returnErr)
	block, err := blockByHashRespectingWatermarks(ctx, a.tmClient, a.watermarks, blockHash[:], 1)
	if err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block.Block.Txs, block.Block.Height), nil
}

func (a *BlockAPI) GetBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool) (result map[string]interface{}, returnErr error) {
	// used for both: eth_ and sei_ namespaces
	return a.getBlockByHash(ctx, blockHash, fullTx, a.includeShellReceipts, nil)
}

func (a *BlockAPI) getBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool, includeSyntheticTxs bool, isPanicTx func(ctx context.Context, hash common.Hash) (bool, error)) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError(fmt.Sprintf("%s_getBlockByHash", a.namespace), a.connectionType, startTime, returnErr)
	block, err := blockByHashRespectingWatermarks(ctx, a.tmClient, a.watermarks, blockHash[:], 1)
	if err != nil {
		return nil, err
	}

	// Validate EVM block height for pacific-1 chain
	sdkCtx := a.ctxProvider(LatestCtxHeight)
	if err := ValidateEVMBlockHeight(sdkCtx.ChainID(), block.Block.Height); err != nil {
		return nil, err
	}

	blockRes, err := blockResultsWithRetry(ctx, a.tmClient, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	return EncodeTmBlock(a.ctxProvider, a.txConfigProvider, block, blockRes, a.keeper, fullTx, a.includeBankTransfers, includeSyntheticTxs, isPanicTx, a.globalBlockCache, a.cacheCreationMutex)
}

func (a *BlockAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError(fmt.Sprintf("%s_getBlockByNumber", a.namespace), a.connectionType, startTime, returnErr)
	if number == 0 {
		// for compatibility with the graph, always return genesis block
		return map[string]interface{}{
			"number":           (*hexutil.Big)(big.NewInt(0)),
			"hash":             "0xF9D3845DF25B43B1C6926F3CEDA6845C17F5624E12212FD8847D0BA01DA1AB9E",
			"parentHash":       common.Hash{},
			"nonce":            ethtypes.BlockNonce{},   // inapplicable to Sei
			"mixHash":          common.Hash{},           // inapplicable to Sei
			"sha3Uncles":       ethtypes.EmptyUncleHash, // inapplicable to Sei
			"logsBloom":        ethtypes.Bloom{},
			"stateRoot":        common.Hash{},
			"miner":            common.Address{},
			"difficulty":       (*hexutil.Big)(big.NewInt(0)), // inapplicable to Sei
			"extraData":        hexutil.Bytes{},               // inapplicable to Sei
			"gasLimit":         hexutil.Uint64(0),
			"gasUsed":          hexutil.Uint64(0),
			"timestamp":        hexutil.Uint64(0),
			"transactionsRoot": common.Hash{},
			"receiptsRoot":     common.Hash{},
			"size":             hexutil.Uint64(0),
			"uncles":           []common.Hash{}, // inapplicable to Sei
			"transactions":     []interface{}{},
			"baseFeePerGas":    (*hexutil.Big)(big.NewInt(0)),
		}, nil
	}
	return a.getBlockByNumber(ctx, number, fullTx, a.includeShellReceipts, nil)
}

func (a *BlockAPI) getBlockByNumber(
	ctx context.Context,
	number rpc.BlockNumber,
	fullTx bool,
	includeSyntheticTxs bool,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
) (result map[string]interface{}, returnErr error) {
	numberPtr, err := getBlockNumber(ctx, a.tmClient, number)
	if err != nil {
		return nil, err
	}

	// Validate EVM block height for pacific-1 chain
	if numberPtr != nil {
		sdkCtx := a.ctxProvider(LatestCtxHeight)
		if err := ValidateEVMBlockHeight(sdkCtx.ChainID(), *numberPtr); err != nil {
			return nil, err
		}
	}

	block, err := blockByNumberRespectingWatermarks(ctx, a.tmClient, a.watermarks, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	blockRes, err := blockResultsWithRetry(ctx, a.tmClient, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	return EncodeTmBlock(a.ctxProvider, a.txConfigProvider, block, blockRes, a.keeper, fullTx, a.includeBankTransfers, includeSyntheticTxs, isPanicTx, a.globalBlockCache, a.cacheCreationMutex)
}

func (a *BlockAPI) GetBlockReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (result []map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError(fmt.Sprintf("%s_getBlockReceipts", a.namespace), a.connectionType, startTime, returnErr)
	// Get height from params
	heightPtr, err := GetBlockNumberByNrOrHash(ctx, a.tmClient, a.watermarks, blockNrOrHash)
	if err != nil {
		return nil, err
	}

	block, err := blockByNumberRespectingWatermarks(ctx, a.tmClient, a.watermarks, heightPtr, 1)
	if err != nil {
		return nil, err
	}

	// Get all tx hashes for the block
	height := block.Block.Height

	txHashes := getTxHashesFromBlock(a.ctxProvider, a.txConfigProvider, a.keeper, block, shouldIncludeSynthetic(a.namespace), a.cacheCreationMutex, a.globalBlockCache)

	// Get tx receipts for all hashes in parallel
	wg := sync.WaitGroup{}
	mtx := sync.Mutex{}
	allReceipts := make([]map[string]interface{}, len(txHashes))
	for i, hash := range txHashes {
		wg.Add(1)
		go func(i int, hash typedTxHash) {
			defer wg.Done()
			defer recoverAndLog()
			receipt, err := a.keeper.GetReceipt(a.ctxProvider(height), hash.hash)
			if err != nil {
				// When the transaction doesn't exist, skip it
				if !strings.Contains(err.Error(), "not found") {
					mtx.Lock()
					returnErr = err
					mtx.Unlock()
				}
			} else {
				encodedReceipt, err := encodeReceipt(a.ctxProvider, a.txConfigProvider, receipt, a.keeper, block, a.includeShellReceipts, a.globalBlockCache, a.cacheCreationMutex)
				if err != nil {
					mtx.Lock()
					returnErr = err
					mtx.Unlock()
				}
				allReceipts[i] = encodedReceipt
			}
		}(i, hash)
	}
	wg.Wait()
	compactReceipts := make([]map[string]interface{}, 0)
	for _, r := range allReceipts {
		if len(r) > 0 {
			compactReceipts = append(compactReceipts, r)
		}
	}
	for i, cr := range compactReceipts {
		cr["transactionIndex"] = hexutil.Uint64(i) //nolint:gosec
	}
	if returnErr != nil {
		return nil, returnErr
	}
	return compactReceipts, nil
}

func EncodeTmBlock(
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	block *coretypes.ResultBlock,
	blockRes *coretypes.ResultBlockResults,
	k *keeper.Keeper,
	fullTx bool,
	includeBankTransfers bool,
	includeSyntheticTxs bool,
	isPanicOrSynthetic func(ctx context.Context, hash common.Hash) (bool, error),
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
) (map[string]interface{}, error) {
	number := big.NewInt(block.Block.Height)
	blockhash := common.HexToHash(block.BlockID.Hash.String())
	blockTime := block.Block.Time
	lastHash := common.HexToHash(block.Block.LastBlockID.Hash.String())
	appHash := common.HexToHash(block.Block.AppHash.String())
	txHash := common.HexToHash(block.Block.DataHash.String())
	resultHash := common.HexToHash(block.Block.LastResultsHash.String())
	miner := common.HexToAddress(block.Block.ProposerAddress.String())
	ctx := ctxProvider(block.Block.Height)
	var baseFeePerGas *big.Int
	if block.Block.Height > 1 {
		baseFeePerGas = k.GetNextBaseFeePerGas(ctxProvider(block.Block.Height - 1)).TruncateInt().BigInt()
	} else {
		baseFeePerGas = types.DefaultMinFeePerGas.TruncateInt().BigInt()
	}
	var blockGasUsed int64
	chainConfig := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	transactions := []interface{}{}
	latestCtx := ctxProvider(LatestCtxHeight)

	msgs := filterTransactions(k, ctxProvider, txConfigProvider, block, includeSyntheticTxs, includeBankTransfers, cacheCreationMutex, globalBlockCache)

	blockBloom := make([]byte, ethtypes.BloomByteLength)
	for _, msg := range msgs {
		switch m := msg.msg.(type) {
		case *types.MsgEVMTransaction:
			ethtx, _ := m.AsTransaction()
			hash := ethtx.Hash()
			receipt, _ := k.GetReceipt(latestCtx, hash)
			if !fullTx {
				transactions = append(transactions, hash.Hex())
			} else {
				blockUnix := toUint64(blockTime.Unix())
				newTx := export.NewRPCTransaction(ethtx, blockhash, number.Uint64(), blockUnix, uint64(len(transactions)), baseFeePerGas, chainConfig)
				replaceFrom(newTx, receipt)
				transactions = append(transactions, newTx)
			}
			or := make([]byte, ethtypes.BloomByteLength)
			bloom := ethtypes.Bloom{}
			bloom.SetBytes(receipt.LogsBloom)
			bitutil.ORBytes(or, blockBloom, bloom[:])
			blockBloom = or
			// derive gas used from receipt as TxResult.GasUsed may not be accurate
			// for ante-failing EVM txs.
			blockGasUsed += int64(receipt.GasUsed) //nolint:gosec
		case *wasmtypes.MsgExecuteContract:
			th := sha256.Sum256(block.Block.Txs[msg.index])
			receipt, _ := k.GetReceipt(latestCtx, th)
			if !fullTx {
				transactions = append(transactions, "0x"+hex.EncodeToString(th[:]))
			} else {
				ti := uint64(len(transactions))
				var to common.Address
				ercAddress, _, exists := k.GetAnyPointeeInfo(ctx, m.Contract)
				if exists {
					to = ercAddress
				} else {
					to = k.GetEVMAddressOrDefault(ctx, sdk.MustAccAddressFromBech32(m.Contract))
				}
				transactions = append(transactions, &export.RPCTransaction{
					BlockHash:        &blockhash,
					BlockNumber:      (*hexutil.Big)(number),
					From:             common.HexToAddress(receipt.From),
					To:               &to,
					Input:            m.Msg.Bytes(),
					Hash:             th,
					TransactionIndex: (*hexutil.Uint64)(&ti),
				})
			}
			or := make([]byte, ethtypes.BloomByteLength)
			bloom := ethtypes.Bloom{}
			bloom.SetBytes(receipt.LogsBloom)
			bitutil.ORBytes(or, blockBloom, bloom[:])
			blockBloom = or
			blockGasUsed += blockRes.TxsResults[msg.index].GasUsed
		case *banktypes.MsgSend:
			th := sha256.Sum256(block.Block.Txs[msg.index])
			if !fullTx {
				transactions = append(transactions, "0x"+hex.EncodeToString(th[:]))
			} else {
				rpcTx := &export.RPCTransaction{
					BlockHash:   &blockhash,
					BlockNumber: (*hexutil.Big)(number),
					Hash:        th,
				}
				senderSeiAddr, _ := sdk.AccAddressFromBech32(m.FromAddress)
				rpcTx.From = k.GetEVMAddressOrDefault(ctx, senderSeiAddr)
				recipientSeiAddr, _ := sdk.AccAddressFromBech32(m.ToAddress)
				recipientEvmAddr := k.GetEVMAddressOrDefault(ctx, recipientSeiAddr)
				rpcTx.To = &recipientEvmAddr
				amt := m.Amount.AmountOf("usei").Mul(state.SdkUseiToSweiMultiplier)
				rpcTx.Value = (*hexutil.Big)(amt.BigInt())
				ti := uint64(len(transactions))
				rpcTx.TransactionIndex = (*hexutil.Uint64)(&ti)
				transactions = append(transactions, rpcTx)
			}
			blockGasUsed += blockRes.TxsResults[msg.index].GasUsed
		}
	}
	if len(transactions) == 0 {
		txHash = ethtypes.EmptyTxsHash
	}

	gasLimit := blockRes.ConsensusParamUpdates.Block.MaxGas
	result := map[string]interface{}{
		"number":           (*hexutil.Big)(number),
		"hash":             blockhash,
		"parentHash":       lastHash,
		"nonce":            ethtypes.BlockNonce{},   // inapplicable to Sei
		"mixHash":          common.Hash{},           // inapplicable to Sei
		"sha3Uncles":       ethtypes.EmptyUncleHash, // inapplicable to Sei
		"logsBloom":        ethtypes.BytesToBloom(blockBloom),
		"stateRoot":        appHash,
		"miner":            miner,
		"difficulty":       (*hexutil.Big)(big.NewInt(0)),           // inapplicable to Sei
		"extraData":        hexutil.Bytes{},                         // inapplicable to Sei
		"gasLimit":         hexutil.Uint64(gasLimit),                //nolint:gosec
		"gasUsed":          hexutil.Uint64(blockGasUsed),            //nolint:gosec
		"timestamp":        hexutil.Uint64(block.Block.Time.Unix()), //nolint:gosec
		"transactionsRoot": txHash,
		"receiptsRoot":     resultHash,
		"size":             hexutil.Uint64(block.Block.Size()), //nolint:gosec
		"uncles":           []common.Hash{},                    // inapplicable to Sei
		"transactions":     transactions,
		"baseFeePerGas":    (*hexutil.Big)(baseFeePerGas),
	}
	if fullTx {
		result["totalDifficulty"] = (*hexutil.Big)(big.NewInt(0)) // inapplicable to Sei
	}
	return result, nil
}

func FullBloom() ethtypes.Bloom {
	bz := []byte{}
	for i := 0; i < ethtypes.BloomByteLength; i++ {
		bz = append(bz, 255)
	}
	return ethtypes.BytesToBloom(bz)
}

// filters out non-evm txs
func (a *BlockAPI) getEvmTxCount(txs tmtypes.Txs, height int64) *hexutil.Uint {
	cnt := 0
	// Only count eth txs
	for _, tx := range txs {
		ethtx := getEthTxForTxBz(tx, a.txConfigProvider(height).TxDecoder())
		if ethtx != nil {
			cnt += 1
		}

	}
	cntHex := hexutil.Uint(cnt) //nolint:gosec
	return &cntHex
}

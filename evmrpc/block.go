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

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/bitutil"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
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
}

type SeiBlockAPI struct {
	*BlockAPI
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error)
}

func NewBlockAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, connectionType ConnectionType) *BlockAPI {
	return &BlockAPI{
		tmClient:             tmClient,
		keeper:               k,
		ctxProvider:          ctxProvider,
		txConfigProvider:     txConfigProvider,
		connectionType:       connectionType,
		includeShellReceipts: false,
		includeBankTransfers: false,
		namespace:            EthNamespace,
	}
}

func NewSeiBlockAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	connectionType ConnectionType,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
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
) *SeiBlockAPI {
	blockAPI := NewSeiBlockAPI(tmClient, k, ctxProvider, txConfigProvider, connectionType, isPanicTx)
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
	block, err := blockByNumberWithRetry(ctx, a.tmClient, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block.Block.Txs, block.Block.Height), nil
}

func (a *BlockAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) (result *hexutil.Uint, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError(fmt.Sprintf("%s_getBlockTransactionCountByHash", a.namespace), a.connectionType, startTime, returnErr)
	block, err := blockByHashWithRetry(ctx, a.tmClient, blockHash[:], 1)
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
	block, err := blockByHashWithRetry(ctx, a.tmClient, blockHash[:], 1)
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
	return EncodeTmBlock(a.ctxProvider, a.txConfigProvider, block, blockRes, a.keeper, fullTx, a.includeBankTransfers, includeSyntheticTxs, isPanicTx)
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

	block, err := blockByNumberWithRetry(ctx, a.tmClient, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	blockRes, err := blockResultsWithRetry(ctx, a.tmClient, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	return EncodeTmBlock(a.ctxProvider, a.txConfigProvider, block, blockRes, a.keeper, fullTx, a.includeBankTransfers, includeSyntheticTxs, isPanicTx)
}

func (a *BlockAPI) GetBlockReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (result []map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError(fmt.Sprintf("%s_getBlockReceipts", a.namespace), a.connectionType, startTime, returnErr)
	// Get height from params
	heightPtr, err := GetBlockNumberByNrOrHash(ctx, a.tmClient, blockNrOrHash)
	if err != nil {
		return nil, err
	}

	block, err := blockByNumberWithRetry(ctx, a.tmClient, heightPtr, 1)
	if err != nil {
		return nil, err
	}

	// Get all tx hashes for the block
	sdkCtx := a.ctxProvider(LatestCtxHeight)
	height := block.Block.Height
	signer := ethtypes.MakeSigner(
		types.DefaultChainConfig().EthereumConfig(a.keeper.ChainID(sdkCtx)),
		big.NewInt(sdkCtx.BlockHeight()),
		uint64(sdkCtx.BlockTime().Unix()), //nolint:gosec
	)
	txHashes := getTxHashesFromBlock(a.ctxProvider, a.txConfigProvider, a.keeper, block, signer, shouldIncludeSynthetic(a.namespace))
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
				encodedReceipt, err := encodeReceipt(a.ctxProvider, a.txConfigProvider, receipt, a.keeper, block, a.includeShellReceipts, signer)
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
	signer := ethtypes.MakeSigner(
		types.DefaultChainConfig().EthereumConfig(k.ChainID(latestCtx)),
		big.NewInt(latestCtx.BlockHeight()),
		uint64(latestCtx.BlockTime().Unix()), //nolint:gosec
	)
	msgs := filterTransactions(k, ctxProvider, txConfigProvider, block, signer, includeSyntheticTxs, includeBankTransfers)

	blockBloom := make([]byte, ethtypes.BloomByteLength)
	for _, msg := range msgs {
		switch m := msg.msg.(type) {
		case *types.MsgEVMTransaction:
			ethtx, _ := m.AsTransaction()
			hash := ethtx.Hash()
			if !fullTx {
				transactions = append(transactions, hash.Hex())
			} else {
				newTx := export.NewRPCTransaction(ethtx, blockhash, number.Uint64(), uint64(blockTime.Second()), uint64(len(transactions)), baseFeePerGas, chainConfig) //nolint:gosec
				transactions = append(transactions, newTx)
			}
			receipt, _ := k.GetReceipt(latestCtx, hash)
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

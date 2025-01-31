package evmrpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

const ShellEVMTxType = math.MaxUint32

type BlockAPI struct {
	tmClient             rpcclient.Client
	keeper               *keeper.Keeper
	ctxProvider          func(int64) sdk.Context
	txConfig             client.TxConfig
	connectionType       ConnectionType
	namespace            string
	includeShellReceipts bool
	includeBankTransfers bool
}

type SeiBlockAPI struct {
	*BlockAPI
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error)
}

func NewBlockAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfig client.TxConfig, connectionType ConnectionType) *BlockAPI {
	return &BlockAPI{
		tmClient:             tmClient,
		keeper:               k,
		ctxProvider:          ctxProvider,
		txConfig:             txConfig,
		connectionType:       connectionType,
		includeShellReceipts: false,
		includeBankTransfers: false,
		namespace:            "eth",
	}
}

func NewSeiBlockAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfig client.TxConfig,
	connectionType ConnectionType,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
) *SeiBlockAPI {
	blockAPI := &BlockAPI{
		tmClient:             tmClient,
		keeper:               k,
		ctxProvider:          ctxProvider,
		txConfig:             txConfig,
		connectionType:       connectionType,
		includeShellReceipts: true,
		includeBankTransfers: false,
		namespace:            "sei",
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
	txConfig client.TxConfig,
	connectionType ConnectionType,
	isPanicTx func(ctx context.Context, hash common.Hash) (bool, error),
) *SeiBlockAPI {
	blockAPI := NewSeiBlockAPI(tmClient, k, ctxProvider, txConfig, connectionType, isPanicTx)
	blockAPI.namespace = "sei2"
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
	defer recordMetrics(fmt.Sprintf("%s_getBlockTransactionCountByNumber", a.namespace), a.connectionType, startTime, returnErr == nil)
	numberPtr, err := getBlockNumber(ctx, a.tmClient, number)
	if err != nil {
		return nil, err
	}
	block, err := blockByNumberWithRetry(ctx, a.tmClient, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block.Block.Txs), nil
}

func (a *BlockAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) (result *hexutil.Uint, returnErr error) {
	startTime := time.Now()
	defer recordMetrics(fmt.Sprintf("%s_getBlockTransactionCountByHash", a.namespace), a.connectionType, startTime, returnErr == nil)
	block, err := blockByHashWithRetry(ctx, a.tmClient, blockHash[:], 1)
	if err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block.Block.Txs), nil
}

func (a *BlockAPI) GetBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool) (result map[string]interface{}, returnErr error) {
	// used for both: eth_ and sei_ namespaces
	return a.getBlockByHash(ctx, blockHash, fullTx, a.includeShellReceipts, nil)
}

func (a *BlockAPI) getBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool, includeSyntheticTxs bool, isPanicTx func(ctx context.Context, hash common.Hash) (bool, error)) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics(fmt.Sprintf("%s_getBlockByHash", a.namespace), a.connectionType, startTime, returnErr == nil)
	block, err := blockByHashWithRetry(ctx, a.tmClient, blockHash[:], 1)
	if err != nil {
		return nil, err
	}
	blockRes, err := blockResultsWithRetry(ctx, a.tmClient, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	blockBloom := a.keeper.GetBlockBloom(a.ctxProvider(block.Block.Height))
	return EncodeTmBlock(a.ctxProvider(block.Block.Height), block, blockRes, blockBloom, a.keeper, a.txConfig.TxDecoder(), fullTx, a.includeBankTransfers, includeSyntheticTxs, isPanicTx)
}

func (a *BlockAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics(fmt.Sprintf("%s_getBlockByNumber", a.namespace), a.connectionType, startTime, returnErr == nil)
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
	startTime := time.Now()
	defer recordMetrics(fmt.Sprintf("%s_getBlockByNumber", a.namespace), a.connectionType, startTime, returnErr == nil)
	numberPtr, err := getBlockNumber(ctx, a.tmClient, number)
	if err != nil {
		return nil, err
	}
	block, err := blockByNumberWithRetry(ctx, a.tmClient, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	blockRes, err := blockResultsWithRetry(ctx, a.tmClient, &block.Block.Height)
	if err != nil {
		return nil, err
	}
	blockBloom := a.keeper.GetBlockBloom(a.ctxProvider(block.Block.Height))
	return EncodeTmBlock(a.ctxProvider(block.Block.Height), block, blockRes, blockBloom, a.keeper, a.txConfig.TxDecoder(), fullTx, a.includeBankTransfers, includeSyntheticTxs, isPanicTx)
}

func (a *BlockAPI) GetBlockReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (result []map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer recordMetrics(fmt.Sprintf("%s_getBlockReceipts", a.namespace), a.connectionType, startTime, returnErr == nil)
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
	height := block.Block.Header.Height
	txHashes := getTxHashesFromBlock(block, a.txConfig, shouldIncludeSynthetic(a.namespace))
	// Get tx receipts for all hashes in parallel
	wg := sync.WaitGroup{}
	mtx := sync.Mutex{}
	allReceipts := make([]map[string]interface{}, len(txHashes))
	for i, hash := range txHashes {
		wg.Add(1)
		go func(i int, hash common.Hash) {
			defer wg.Done()
			receipt, err := a.keeper.GetReceipt(a.ctxProvider(height), hash)
			if err != nil {
				// When the transaction doesn't exist, skip it
				if !strings.Contains(err.Error(), "not found") {
					mtx.Lock()
					returnErr = err
					mtx.Unlock()
				}
			} else {
				// If the receipt has synthetic logs, we actually want to include them in the response.
				if !a.includeShellReceipts && receipt.TxType == ShellEVMTxType {
					return
				}
				// tx hash is included in a future block (because it failed in the current block due to
				// checks before the account's nonce is updated)
				if receipt.BlockNumber != uint64(height) {
					return
				}
				encodedReceipt, err := encodeReceipt(receipt, a.txConfig.TxDecoder(), block, func(h common.Hash) bool {
					_, err := a.keeper.GetReceipt(a.ctxProvider(height), h)
					return err == nil
				})
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
	if returnErr != nil {
		return nil, returnErr
	}
	return compactReceipts, nil
}

func EncodeTmBlock(
	ctx sdk.Context,
	block *coretypes.ResultBlock,
	blockRes *coretypes.ResultBlockResults,
	blockBloom ethtypes.Bloom,
	k *keeper.Keeper,
	txDecoder sdk.TxDecoder,
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
	baseFeePerGas := k.GetDynamicBaseFeePerGas(ctx).TruncateInt().BigInt()
	var blockGasUsed int64
	chainConfig := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	transactions := []interface{}{}

	for i, txRes := range blockRes.TxsResults {
		blockGasUsed += txRes.GasUsed
		decoded, err := txDecoder(block.Block.Txs[i])
		if err != nil {
			return nil, errors.New("failed to decode transaction")
		}
		for _, msg := range decoded.GetMsgs() {
			switch m := msg.(type) {
			case *types.MsgEVMTransaction:
				if m.IsAssociateTx() {
					continue
				}
				ethtx, _ := m.AsTransaction()
				hash := ethtx.Hash()
				if isPanicOrSynthetic != nil {
					isPanicOrSynthetic, err := isPanicOrSynthetic(ctx.Context(), hash)
					if err != nil {
						return nil, fmt.Errorf("failed to check if tx is panic tx: %w", err)
					}
					if isPanicOrSynthetic {
						continue
					}
				}
				receipt, err := k.GetReceipt(ctx, hash)
				if err != nil {
					continue
				}
				if !includeSyntheticTxs && receipt.TxType == ShellEVMTxType {
					continue
				}
				if !fullTx {
					transactions = append(transactions, hash)
				} else {
					newTx := ethapi.NewRPCTransaction(ethtx, blockhash, number.Uint64(), uint64(blockTime.Second()), uint64(receipt.TransactionIndex), baseFeePerGas, chainConfig)
					transactions = append(transactions, newTx)
				}
			case *wasmtypes.MsgExecuteContract:
				if !includeSyntheticTxs {
					continue
				}
				th := sha256.Sum256(block.Block.Txs[i])
				receipt, err := k.GetReceipt(ctx, th)
				if err != nil {
					continue
				}
				if !fullTx {
					transactions = append(transactions, "0x"+hex.EncodeToString(th[:]))
				} else {
					ti := uint64(receipt.TransactionIndex)
					to := k.GetEVMAddressOrDefault(ctx, sdk.MustAccAddressFromBech32(m.Contract))
					transactions = append(transactions, &ethapi.RPCTransaction{
						BlockHash:        &blockhash,
						BlockNumber:      (*hexutil.Big)(number),
						From:             common.HexToAddress(receipt.From),
						To:               &to,
						Input:            m.Msg.Bytes(),
						Hash:             th,
						TransactionIndex: (*hexutil.Uint64)(&ti),
					})
				}
			case *banktypes.MsgSend:
				if !includeBankTransfers {
					continue
				}
				th := sha256.Sum256(block.Block.Txs[i])
				if !fullTx {
					transactions = append(transactions, "0x"+hex.EncodeToString(th[:]))
				} else {
					rpcTx := &ethapi.RPCTransaction{
						BlockHash:   &blockhash,
						BlockNumber: (*hexutil.Big)(number),
						Hash:        th,
					}
					senderSeiAddr, err := sdk.AccAddressFromBech32(m.FromAddress)
					if err != nil {
						continue
					}
					rpcTx.From = k.GetEVMAddressOrDefault(ctx, senderSeiAddr)
					recipientSeiAddr, err := sdk.AccAddressFromBech32(m.ToAddress)
					if err != nil {
						continue
					}
					recipientEvmAddr := k.GetEVMAddressOrDefault(ctx, recipientSeiAddr)
					rpcTx.To = &recipientEvmAddr
					amt := m.Amount.AmountOf("usei").Mul(state.SdkUseiToSweiMultiplier)
					rpcTx.Value = (*hexutil.Big)(amt.BigInt())
					transactions = append(transactions, rpcTx)
				}
			}
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
		"logsBloom":        blockBloom,
		"stateRoot":        appHash,
		"miner":            miner,
		"difficulty":       (*hexutil.Big)(big.NewInt(0)), // inapplicable to Sei
		"extraData":        hexutil.Bytes{},               // inapplicable to Sei
		"gasLimit":         hexutil.Uint64(gasLimit),
		"gasUsed":          hexutil.Uint64(blockGasUsed),
		"timestamp":        hexutil.Uint64(block.Block.Time.Unix()),
		"transactionsRoot": txHash,
		"receiptsRoot":     resultHash,
		"size":             hexutil.Uint64(block.Block.Size()),
		"uncles":           []common.Hash{}, // inapplicable to Sei
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
func (a *BlockAPI) getEvmTxCount(txs tmtypes.Txs) *hexutil.Uint {
	cnt := 0
	// Only count eth txs
	for _, tx := range txs {
		ethtx := getEthTxForTxBz(tx, a.txConfig.TxDecoder())
		if ethtx != nil {
			cnt += 1
		}

	}
	cntHex := hexutil.Uint(cnt)
	return &cntHex
}

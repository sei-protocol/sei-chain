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
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"golang.org/x/sync/errgroup"
)

const (
	EthNamespace  = "eth"
	SeiNamespace  = "sei"
	Sei2Namespace = "sei2"
)

// maxBlockReceiptsConcurrency is a hard cap on the number of goroutines
// eth_getBlockReceipts (and its sei_/sei2_ variants) will fan out to when
// fetching per-tx receipts.
const maxBlockReceiptsConcurrency = 100

// genesisBlockHashHex is the block hash returned by GetBlockByNumber("0x0"). Hash-based lookups
// must recognize this so that count/block-by-hash stay consistent with block-by-number.
const genesisBlockHashHex = "0xF9D3845DF25B43B1C6926F3CEDA6845C17F5624E12212FD8847D0BA01DA1AB9E"

var genesisBlockHash = common.HexToHash(genesisBlockHashHex)

// genesisBlockTxCount is the transaction count for the synthetic genesis block (eth_getBlockTransactionCountByHash/ByNumber for genesis).
var genesisBlockTxCount = func() *hexutil.Uint { u := hexutil.Uint(0); return &u }()

func encodeGenesisBlock() map[string]any {
	return map[string]any{
		"number":           (*hexutil.Big)(big.NewInt(0)),
		"hash":             genesisBlockHashHex,
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
		"transactions":     []any{},
		"baseFeePerGas":    (*hexutil.Big)(big.NewInt(0)),
	}
}

type BlockAPI struct {
	tmClient             client.LocalClient
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
}

func NewBlockAPI(tmClient client.LocalClient, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfigProvider func(int64) client.TxConfig, connectionType ConnectionType, watermarks *WatermarkManager, globalBlockCache BlockCache, cacheCreationMutex *sync.Mutex) *BlockAPI {
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
	tmClient client.LocalClient,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	connectionType ConnectionType,
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
		BlockAPI: blockAPI,
	}
}

func NewSei2BlockAPI(
	tmClient client.LocalClient,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	connectionType ConnectionType,
	watermarks *WatermarkManager,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
) *SeiBlockAPI {
	blockAPI := NewSeiBlockAPI(tmClient, k, ctxProvider, txConfigProvider, connectionType, watermarks, globalBlockCache, cacheCreationMutex)
	blockAPI.namespace = Sei2Namespace
	blockAPI.includeBankTransfers = true
	return blockAPI
}

func (a *SeiBlockAPI) GetBlockByNumberExcludeTraceFail(ctx context.Context, number rpc.BlockNumber, fullTx bool) (result map[string]interface{}, returnErr error) {
	// Match eth_getBlockByNumber("0x0"): synthetic genesis, not the Tendermint block at height 0.
	if number == 0 {
		return encodeGenesisBlock(), nil
	}
	// Exclude synthetic txs (filterTransactions drops them) and ante-failure
	// stub receipts (EncodeTmBlock drops them via excludeUntraceable).
	return a.getBlockByNumber(ctx, number, fullTx, false, true)
}

func (a *SeiBlockAPI) GetBlockByHashExcludeTraceFail(ctx context.Context, blockHash common.Hash, fullTx bool) (result map[string]interface{}, returnErr error) {
	// See note on GetBlockByNumberExcludeTraceFail.
	return a.getBlockByHash(ctx, blockHash, fullTx, false, true)
}

func (a *BlockAPI) GetBlockTransactionCountByNumber(ctx context.Context, number rpc.BlockNumber) (result *hexutil.Uint, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, fmt.Sprintf("%s_getBlockTransactionCountByNumber", a.namespace), a.connectionType, startTime, returnErr, recover())
	}()
	if number == 0 {
		return genesisBlockTxCount, nil
	}
	numberPtr, err := getBlockNumber(ctx, a.tmClient, number)
	if err != nil {
		return nil, err
	}
	// Ethereum JSON-RPC: non-existent / future numeric block => null, not an error.
	block, err := blockByNumberOrNullForJSONRPC(ctx, a.tmClient, a.watermarks, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}
	if err = a.watermarks.EnsureReceiptHeightAvailable(block.Block.Height); err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block), nil
}

func (a *BlockAPI) GetBlockTransactionCountByHash(ctx context.Context, blockHash common.Hash) (result *hexutil.Uint, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, fmt.Sprintf("%s_getBlockTransactionCountByHash", a.namespace), a.connectionType, startTime, returnErr, recover())
	}()
	if blockHash == genesisBlockHash {
		return genesisBlockTxCount, nil
	}
	// Ethereum JSON-RPC: non-existent block hash => null, not an error.
	block, err := blockByHashOrNullForJSONRPC(ctx, a.tmClient, a.watermarks, blockHash[:], 1)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}
	if err = a.watermarks.EnsureReceiptHeightAvailable(block.Block.Height); err != nil {
		return nil, err
	}
	return a.getEvmTxCount(block), nil
}

func (a *BlockAPI) GetBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool) (result map[string]interface{}, returnErr error) {
	// used for both: eth_ and sei_ namespaces
	return a.getBlockByHash(ctx, blockHash, fullTx, a.includeShellReceipts, false)
}

func (a *BlockAPI) getBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool, includeSyntheticTxs bool, excludeUntraceable bool) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, fmt.Sprintf("%s_getBlockByHash", a.namespace), a.connectionType, startTime, returnErr, recover())
	}()

	// Ethereum spec: empty or non-existent block hash returns result=null, not error.
	if blockHash == (common.Hash{}) {
		return nil, nil
	}
	if blockHash == genesisBlockHash {
		return encodeGenesisBlock(), nil
	}
	// Ethereum JSON-RPC: non-existent block hash (unknown OR above safe latest)
	// => null, not an error. The helper handles both cases.
	block, err := blockByHashOrNullForJSONRPC(ctx, a.tmClient, a.watermarks, blockHash[:], 1)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}

	// Validate EVM block height for pacific-1 chain
	sdkCtx := a.ctxProvider(LatestCtxHeight)
	if err := ValidateEVMBlockHeight(sdkCtx.ChainID(), block.Block.Height); err != nil {
		return nil, err
	}

	return EncodeTmBlock(a.ctxProvider, a.txConfigProvider, block, a.keeper, fullTx, a.includeBankTransfers, includeSyntheticTxs, excludeUntraceable, a.globalBlockCache, a.cacheCreationMutex)
}

func (a *BlockAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (result map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, fmt.Sprintf("%s_getBlockByNumber", a.namespace), a.connectionType, startTime, returnErr, recover())
	}()
	if number == 0 {
		// for compatibility with the graph, always return genesis block
		return encodeGenesisBlock(), nil
	}
	return a.getBlockByNumber(ctx, number, fullTx, a.includeShellReceipts, false)
}

func (a *BlockAPI) getBlockByNumber(
	ctx context.Context,
	number rpc.BlockNumber,
	fullTx bool,
	includeSyntheticTxs bool,
	excludeUntraceable bool,
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

	// Ethereum JSON-RPC: non-existent / future numeric block => null, not an error.
	block, err := blockByNumberOrNullForJSONRPC(ctx, a.tmClient, a.watermarks, numberPtr, 1)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}
	return EncodeTmBlock(a.ctxProvider, a.txConfigProvider, block, a.keeper, fullTx, a.includeBankTransfers, includeSyntheticTxs, excludeUntraceable, a.globalBlockCache, a.cacheCreationMutex)
}

func (a *BlockAPI) GetBlockReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (result []map[string]interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, fmt.Sprintf("%s_getBlockReceipts", a.namespace), a.connectionType, startTime, returnErr, recover())
	}()
	// Ethereum spec: empty or non-existent block hash returns result=null, not error.
	if blockNrOrHash.BlockHash != nil && *blockNrOrHash.BlockHash == (common.Hash{}) {
		return nil, nil
	}
	// Synthetic genesis (eth_getBlockByNumber("0x0")): empty receipts without TM/watermarks.
	// Callers may pass the genesis hash or the literal block number 0x0 (parsed as number, not hash).
	if blockNrOrHash.BlockHash != nil && *blockNrOrHash.BlockHash == genesisBlockHash {
		return []map[string]any{}, nil
	}
	if blockNrOrHash.BlockNumber != nil && *blockNrOrHash.BlockNumber == 0 {
		return []map[string]any{}, nil
	}
	// Ethereum JSON-RPC: non-existent / above-watermark block => null, not an error.
	// Dispatch on hash vs number directly so a nil heightPtr from getBlockNumber
	// (the "latest"/"safe"/"finalized"/"pending" tags) resolves to the safe-latest
	// height via blockByNumberOrNullForJSONRPC rather than being misread as
	// "block doesn't exist".
	var (
		block *coretypes.ResultBlock
		err   error
	)
	if blockNrOrHash.BlockHash != nil {
		block, err = blockByHashOrNullForJSONRPC(ctx, a.tmClient, a.watermarks, blockNrOrHash.BlockHash[:], 1)
	} else {
		var numberPtr *int64
		if numberPtr, err = getBlockNumber(ctx, a.tmClient, *blockNrOrHash.BlockNumber); err == nil {
			block, err = blockByNumberOrNullForJSONRPC(ctx, a.tmClient, a.watermarks, numberPtr, 1)
		}
	}
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}

	// Get all tx hashes for the block
	height := block.Block.Height

	txHashes := getTxHashesFromBlock(a.ctxProvider, a.txConfigProvider, a.keeper, block, shouldIncludeSynthetic(a.namespace), a.cacheCreationMutex, a.globalBlockCache)

	// Get tx receipts for all hashes in parallel, with a hard cap on the
	// goroutine fan-out, so a block with a very large number of txs
	// cannot spawn an unbounded number of goroutines. errgroup.SetLimit blocks
	// Go() until a slot frees, bounding the number of live goroutines rather
	// than just the number doing concurrent work.
	allReceipts := make([]map[string]interface{}, len(txHashes))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxBlockReceiptsConcurrency)
	for i, hash := range txHashes {
		g.Go(func() error {
			defer recoverAndLog()
			// Bail early if a sibling goroutine already errored or the caller
			// cancelled; no point doing work the group is about to discard.
			if err := ctx.Err(); err != nil {
				return err
			}
			receipt, err := getOrSetCachedReceiptErr(a.cacheCreationMutex, a.globalBlockCache, a.ctxProvider(height), a.keeper, block, hash.hash)
			if err != nil {
				// A missing receipt is expected for some hashes and is not an
				// error; skip it and leave allReceipts[i] empty.
				if strings.Contains(err.Error(), "not found") {
					return nil
				}
				return err
			}
			encodedReceipt, err := encodeReceipt(a.ctxProvider, a.txConfigProvider, receipt, a.keeper, block, a.includeShellReceipts, a.globalBlockCache, a.cacheCreationMutex)
			if err != nil {
				return err
			}
			allReceipts[i] = encodedReceipt
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	compactReceipts := make([]map[string]interface{}, 0)
	for _, r := range allReceipts {
		if len(r) > 0 {
			compactReceipts = append(compactReceipts, r)
		}
	}
	for i, cr := range compactReceipts {
		cr["transactionIndex"] = hexutil.Uint64(i) //nolint:gosec
	}
	return compactReceipts, nil
}

// EncodeTmBlock renders a tendermint block as an eth_getBlockBy* response.
//
// excludeUntraceable, when true, drops EVM txs whose receipt is an
// ante-deferred stub (EffectiveGasPrice==0 && GasUsed==0). x/evm/keeper/abci.go
// writes such stubs for txs that passed the nonce check but failed a later
// ante step (insufficient funds, insufficient fee, etc.); they never reached
// the VM and have no meaningful trace. Used by the *ExcludeTraceFail block
// endpoints to satisfy evmrpc/README.md's "included in blocks but not
// executed" filter; the regular eth_getBlockBy* endpoints pass false so
// these txs still surface in normal block responses (per PR #2343's
// TestAnteFailureOthers — users want to see them).
func EncodeTmBlock(
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	block *coretypes.ResultBlock,
	k *keeper.Keeper,
	fullTx bool,
	includeBankTransfers bool,
	includeSyntheticTxs bool,
	excludeUntraceable bool,
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
			receipt, found := getOrSetCachedReceipt(cacheCreationMutex, globalBlockCache, latestCtx, k, block, hash)
			if !found {
				continue
			}
			// Untraceable receipt — tx never reached the VM (ante-deferred
			// stub) or is chain-generated synthetic. filterTransactions's
			// isReceiptFromAnteError only catches the nonce-error subset
			// post-v5.8.0 (per PR #2343, which keeps insufficient-funds
			// receipts visible to the regular eth_getBlockBy* endpoints);
			// *ExcludeTraceFail needs the broader discriminator. See
			// isReceiptUntraceable for the shared definition used at every
			// *ExcludeTraceFail site.
			if excludeUntraceable && isReceiptUntraceable(receipt) {
				continue
			}
			if !fullTx {
				transactions = append(transactions, hash.Hex())
			} else {
				blockUnix := toUint64(blockTime.Unix())
				newTx := export.NewRPCTransaction(ethtx, blockhash, number.Uint64(), blockUnix, uint64(len(transactions)), baseFeePerGas, chainConfig)
				replaceFrom(newTx, receipt)
				transactions = append(transactions, newTx)
			}
			var bloom ethtypes.Bloom
			bloom.SetBytes(receipt.LogsBloom)
			bitutil.ORBytes(blockBloom, blockBloom, bloom[:])
			// derive gas used from receipt as TxResult.GasUsed may not be accurate
			// for ante-failing EVM txs.
			blockGasUsed += int64(receipt.GasUsed) //nolint:gosec
		case *wasmtypes.MsgExecuteContract:
			th := sha256.Sum256(block.Block.Txs[msg.index])
			receipt, _ := getOrSetCachedReceipt(cacheCreationMutex, globalBlockCache, latestCtx, k, block, th)
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
			var bloom ethtypes.Bloom
			bloom.SetBytes(receipt.LogsBloom)
			bitutil.ORBytes(blockBloom, blockBloom, bloom[:])
			blockGasUsed += int64(receipt.GasUsed) //nolint:gosec
		case *banktypes.MsgSend:
			th := sha256.Sum256(block.Block.Txs[msg.index])
			receipt, _ := getOrSetCachedReceipt(cacheCreationMutex, globalBlockCache, latestCtx, k, block, th)
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
			if receipt != nil {
				blockGasUsed += int64(receipt.GasUsed) //nolint:gosec
			}
		}
	}
	if len(transactions) == 0 {
		txHash = ethtypes.EmptyTxsHash
	}

	// Source block.gasLimit from the active ConsensusParams in the SDK
	// context — same place the EVM runtime reads block.gaslimit from
	// (x/evm/keeper/keeper.go's BlockContext.GasLimit), so
	// eth_getBlockByNumber.gasLimit and the GASLIMIT opcode return the
	// same number.
	var gasLimit int64
	if cp := ctx.ConsensusParams(); cp != nil && cp.Block != nil {
		gasLimit = cp.Block.MaxGas
	}
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

// getEvmTxCount returns the same transaction count as EncodeTmBlock exposes: filterTransactions
// plus the same per-msg rules as EncodeTmBlock (EVM messages need GetReceipt to succeed).
func (a *BlockAPI) getEvmTxCount(block *coretypes.ResultBlock) *hexutil.Uint {
	n := countBlockTxsLikeEncodeTmBlock(
		a.ctxProvider,
		a.txConfigProvider,
		block,
		a.keeper,
		a.includeShellReceipts,
		a.includeBankTransfers,
		a.cacheCreationMutex,
		a.globalBlockCache,
	)
	cntHex := hexutil.Uint(n) //nolint:gosec
	return &cntHex
}

func countBlockTxsLikeEncodeTmBlock(
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	block *coretypes.ResultBlock,
	k *keeper.Keeper,
	includeShellReceipts bool,
	includeBankTransfers bool,
	cacheCreationMutex *sync.Mutex,
	globalBlockCache BlockCache,
) int {
	latestCtx := ctxProvider(LatestCtxHeight)
	msgs := filterTransactions(k, ctxProvider, txConfigProvider, block, includeShellReceipts, includeBankTransfers, cacheCreationMutex, globalBlockCache)
	n := 0
	for _, msg := range msgs {
		switch m := msg.msg.(type) {
		case *types.MsgEVMTransaction:
			ethtx, _ := m.AsTransaction()
			if _, found := getOrSetCachedReceipt(cacheCreationMutex, globalBlockCache, latestCtx, k, block, ethtx.Hash()); !found {
				continue
			}
			n++
		case *wasmtypes.MsgExecuteContract:
			n++
		case *banktypes.MsgSend:
			n++
		}
	}
	return n
}

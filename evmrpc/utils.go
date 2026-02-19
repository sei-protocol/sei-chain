package evmrpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/evmrpc/rpcutils"
	"github.com/sei-protocol/sei-chain/evmrpc/stats"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/legacy"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const LatestCtxHeight int64 = -1

// EVM launch block heights for different chains
const Pacific1EVMLaunchHeight int64 = 79123881

// GetBlockNumberByNrOrHash returns the height of the block with the given number or hash.
func GetBlockNumberByNrOrHash(ctx context.Context, tmClient rpcclient.Client, wm *WatermarkManager, blockNrOrHash rpc.BlockNumberOrHash) (*int64, error) {
	if blockNrOrHash.BlockHash != nil {
		block, err := blockByHashRespectingWatermarks(ctx, tmClient, wm, blockNrOrHash.BlockHash[:], 1)
		if err != nil {
			return nil, err
		}
		height := block.Block.Height
		return &height, nil
	}
	return getBlockNumber(ctx, tmClient, *blockNrOrHash.BlockNumber)
}

func getBlockNumber(ctx context.Context, tmClient rpcclient.Client, number rpc.BlockNumber) (*int64, error) {
	var numberPtr *int64
	switch number {
	case rpc.SafeBlockNumber, rpc.FinalizedBlockNumber, rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		numberPtr = nil // requesting Block with nil means the latest block
	case rpc.EarliestBlockNumber:
		genesisRes, err := tmClient.Genesis(ctx)
		if err != nil {
			return nil, err
		}
		TraceTendermintIfApplicable(ctx, "Genesis", []string{}, genesisRes)
		numberPtr = &genesisRes.Genesis.InitialHeight
	default:
		numberI64 := number.Int64()
		numberPtr = &numberI64
	}
	return numberPtr, nil
}

func getHeightFromBigIntBlockNumber(latest int64, blockNumber *big.Int) int64 {
	switch blockNumber.Int64() {
	case rpc.FinalizedBlockNumber.Int64(), rpc.LatestBlockNumber.Int64(), rpc.SafeBlockNumber.Int64(), rpc.PendingBlockNumber.Int64():
		return latest
	default:
		return blockNumber.Int64()
	}
}

// this avoids a gosec lint error rather than just casting
func toUint64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

func getTestKeyring(homeDir string) (keyring.Keyring, error) {
	clientCtx := client.Context{}.WithViper("").WithHomeDir(homeDir)
	clientCtx, err := config.ReadFromClientConfig(clientCtx)
	if err != nil {
		return nil, err
	}
	return client.NewKeyringFromBackend(clientCtx, keyring.BackendTest)
}

func getAddressPrivKeyMap(kb keyring.Keyring) map[string]*ecdsa.PrivateKey {
	res := map[string]*ecdsa.PrivateKey{}
	keys, err := kb.List()
	if err != nil {
		return res
	}
	for _, key := range keys {
		localInfo, ok := key.(keyring.LocalInfo)
		if !ok {
			// will only show local key
			continue
		}
		if localInfo.GetAlgo() != hd.Secp256k1Type {
			fmt.Printf("Skipping address %s because it isn't signed with secp256k1\n", localInfo.Name)
			continue
		}
		priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
		if err != nil {
			continue
		}
		privHex := hex.EncodeToString(priv.Bytes())
		privKey, err := crypto.HexToECDSA(privHex)
		if err != nil {
			continue
		}
		address := crypto.PubkeyToAddress(privKey.PublicKey)
		res[address.Hex()] = privKey
	}
	return res
}

func blockResultsWithRetry(ctx context.Context, client rpcclient.Client, height *int64) (*coretypes.ResultBlockResults, error) {
	blockRes, err := client.BlockResults(ctx, height)
	if err != nil {
		// retry once, since application DB and block DB are not committed atomically so it's possible for
		// receipt to exist while block results aren't committed yet
		time.Sleep(1 * time.Second)
		blockRes, err = client.BlockResults(ctx, height)
		if err != nil {
			return nil, err
		}
	}
	return blockRes, err
}

func blockByNumber(ctx context.Context, client rpcclient.Client, height *int64) (*coretypes.ResultBlock, error) {
	return blockByNumberWithRetry(ctx, client, height, 0)
}

func blockByNumberWithRetry(ctx context.Context, client rpcclient.Client, height *int64, maxRetries int) (*coretypes.ResultBlock, error) {
	blockRes, err := client.Block(ctx, height)
	var retryCount = 0
	for err != nil && retryCount < maxRetries {
		// retry once, since application DB and block DB are not committed atomically so it's possible for
		// receipt to exist while block results aren't committed yet
		time.Sleep(1 * time.Second)
		blockRes, err = client.Block(ctx, height)
		retryCount++
	}
	if err != nil {
		return nil, err
	}
	if blockRes.Block == nil {
		return nil, fmt.Errorf("could not find block for height %d", height)
	}
	TraceTendermintIfApplicable(ctx, "Block", []string{stringifyInt64Ptr(height)}, blockRes)
	return blockRes, err
}

func blockByHash(ctx context.Context, client rpcclient.Client, hash bytes.HexBytes) (*coretypes.ResultBlock, error) {
	return blockByHashWithRetry(ctx, client, hash, 0)
}

func blockByHashWithRetry(ctx context.Context, client rpcclient.Client, hash bytes.HexBytes, maxRetries int) (*coretypes.ResultBlock, error) {
	blockRes, err := client.BlockByHash(ctx, hash)
	var retryCount = 0
	for err != nil && retryCount < maxRetries {
		// retry once, since application DB and block DB are not committed atomically so it's possible for
		// receipt to exist while block results aren't committed yet
		time.Sleep(1 * time.Second)
		blockRes, err = client.BlockByHash(ctx, hash)
		retryCount++
	}
	if err != nil {
		return nil, err
	}
	if blockRes.Block == nil {
		return nil, fmt.Errorf("could not find block for hash %s", hash.String())
	}
	TraceTendermintIfApplicable(ctx, "BlockByHash", []string{hash.String()}, blockRes)
	return blockRes, err
}

// ValidateEVMBlockHeight checks if the requested block height is valid for EVM queries
func ValidateEVMBlockHeight(chainID string, blockHeight int64) error {
	// Only validate for pacific-1 chain
	if chainID != "pacific-1" {
		return nil
	}
	if blockHeight < Pacific1EVMLaunchHeight {
		return fmt.Errorf("EVM is only supported from block %d onwards", Pacific1EVMLaunchHeight)
	}
	return nil
}

type indexedMsg struct {
	msg   sdk.Msg
	index int
}

func filterTransactions(
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	block *coretypes.ResultBlock,
	includeSyntheticTxs bool,
	includeBankTransfers bool,
	cacheCreationMutex *sync.Mutex,
	globalBlockCache BlockCache,
) []indexedMsg {
	txs := []indexedMsg{}
	txCounts := make(map[string]uint64)
	startOfBlockNonce := make(map[string]uint64)
	txConfig := txConfigProvider(block.Block.Height)
	latestCtx := ctxProvider(LatestCtxHeight)
	ctx := ctxProvider(block.Block.Height)
	prevCtx := ctxProvider(block.Block.Height - 1)
	for i, tx := range block.Block.Txs {
		sdkTx, err := txConfig.TxDecoder()(tx)
		if err != nil {
			continue
		}
		for _, msg := range sdkTx.GetMsgs() {
			switch m := msg.(type) {
			case *types.MsgEVMTransaction:
				if m.IsAssociateTx() {
					continue
				}
				ethtx, _ := m.AsTransaction()
				hash := ethtx.Hash()
				sender, _ := rpcutils.RecoverEVMSender(ethtx, block.Block.Height, block.Block.Time.Unix())
				receipt, found := getOrSetCachedReceipt(cacheCreationMutex, globalBlockCache, latestCtx, k, block, hash)
				if !found || receipt.BlockNumber != uint64(block.Block.Height) || isReceiptFromAnteError(ctx, receipt) { //nolint:gosec
					continue
				}
				txCount := txCounts[sender.Hex()]
				if receipt.Status == 0 && receipt.EffectiveGasPrice == 0 {
					// check if the transaction bumped nonce. If not, exclude it
					if _, ok := startOfBlockNonce[sender.Hex()]; !ok {
						startOfBlockNonce[sender.Hex()] = k.GetNonce(prevCtx, common.HexToAddress(sender.Hex()))
					}
					if txCount+startOfBlockNonce[sender.Hex()] != ethtx.Nonce() {
						continue
					}
				}
				if !includeSyntheticTxs && receipt.TxType == types.ShellEVMTxType {
					continue
				}
				txCounts[sender.Hex()] = txCount + 1
				txs = append(txs, indexedMsg{index: i, msg: msg})
			case *wasmtypes.MsgExecuteContract:
				if !includeSyntheticTxs {
					continue
				}
				th := sha256.Sum256(block.Block.Txs[i])
				_, found := getOrSetCachedReceipt(cacheCreationMutex, globalBlockCache, latestCtx, k, block, th)
				if !found {
					continue
				}
				txs = append(txs, indexedMsg{index: i, msg: msg})
			case *banktypes.MsgSend:
				if !includeBankTransfers {
					continue
				}
				txs = append(txs, indexedMsg{index: i, msg: msg})
			}
		}
	}
	return txs
}

func recordMetrics(apiMethod string, connectionType ConnectionType, startTime time.Time) {
	recordMetricsWithError(apiMethod, connectionType, startTime, nil)
}

func recordMetricsWithError(apiMethod string, connectionType ConnectionType, startTime time.Time, err error) {
	// Automatically detect success/failure based on panic state
	panicValue := recover()
	success := panicValue == nil || err != nil

	// these are only metrics that are specifically typed errors for tracking.
	if err != nil {
		metrics.IncrementErrorMetrics(apiMethod, err)
	}

	metrics.IncrementRpcRequestCounter(apiMethod, string(connectionType), success)
	metrics.MeasureRpcRequestLatency(apiMethod, string(connectionType), startTime)
	stats.RecordAPIInvocation(apiMethod, string(connectionType), startTime, success)

	if panicValue != nil {
		panic(panicValue)
	}
}

func CheckVersion(ctx sdk.Context, k *keeper.Keeper) error {
	if !evmExists(ctx, k) {
		return fmt.Errorf("evm module does not exist on height %d", ctx.BlockHeight())
	}
	if !bankExists(ctx, k) {
		return fmt.Errorf("bank module does not exist on height %d", ctx.BlockHeight())
	}
	return nil
}

func bankExists(ctx sdk.Context, k *keeper.Keeper) bool {
	return ctx.KVStore(k.BankKeeper().GetStoreKey()).VersionExists(ctx.BlockHeight())
}

func evmExists(ctx sdk.Context, k *keeper.Keeper) bool {
	return ctx.KVStore(k.GetStoreKey()).VersionExists(ctx.BlockHeight())
}

func shouldIncludeSynthetic(namespace string) bool {
	if namespace != "eth" && namespace != "sei" {
		panic(fmt.Sprintf("unknown namespace %s", namespace))
	}
	return namespace == "sei"
}

type typedTxHash struct {
	hash  common.Hash
	isEvm bool
}

func getTxHashesFromBlock(
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	k *keeper.Keeper,
	block *coretypes.ResultBlock,
	shouldIncludeSynthetic bool,
	cacheCreationMutex *sync.Mutex,
	globalBlockCache BlockCache,
) []typedTxHash {
	txHashes := []typedTxHash{}
	for _, tx := range filterTransactions(k, ctxProvider, txConfigProvider, block, shouldIncludeSynthetic, false, cacheCreationMutex, globalBlockCache) {
		switch tx.msg.(type) {
		case *types.MsgEVMTransaction:
			ethtx, _ := tx.msg.(*types.MsgEVMTransaction).AsTransaction()
			txHashes = append(txHashes, typedTxHash{hash: ethtx.Hash(), isEvm: true})
		case *wasmtypes.MsgExecuteContract:
			txHashes = append(txHashes, typedTxHash{hash: common.Hash(sha256.Sum256(block.Block.Txs[tx.index])), isEvm: false})
		}
	}
	return txHashes
}

func isReceiptFromAnteError(ctx sdk.Context, receipt *types.Receipt) bool {
	// hacky heuristic
	if strings.Compare(ctx.ClosestUpgradeName(), "v5.8.0") < 0 {
		return receipt.EffectiveGasPrice == 0
	}
	return receipt.EffectiveGasPrice == 0 && (strings.Contains(receipt.VmError, core.ErrNonceTooHigh.Error()) ||
		strings.Contains(receipt.VmError, core.ErrNonceTooLow.Error()))
}

type ParallelRunner struct {
	Done  sync.WaitGroup
	Queue chan func()
}

var panicHook atomic.Value

func SetPanicHook(h func(interface{})) {
	panicHook.Store(h)
}

func NewParallelRunner(cnt int, capacity int) *ParallelRunner {
	pr := &ParallelRunner{
		Done:  sync.WaitGroup{},
		Queue: make(chan func(), capacity),
	}
	pr.Done.Add(cnt)
	for i := 0; i < cnt; i++ {
		go func() {
			defer pr.Done.Done()
			defer recoverAndLog()
			for f := range pr.Queue {
				runWithRecovery(f)
			}
		}()
	}
	return pr
}

func runWithRecovery(f func()) {
	defer recoverAndLog()
	f()
}

func recoverAndLog() {
	if e := recover(); e != nil {
		fmt.Printf("Panic recovered: %s\n", e)
		debug.PrintStack()
		if v := panicHook.Load(); v != nil {
			if hook, ok := v.(func(interface{})); ok && hook != nil {
				hook(e)
			}
		}
	}
}

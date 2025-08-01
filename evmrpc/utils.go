package evmrpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"runtime/debug"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/hd"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/tendermint/tendermint/libs/bytes"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

const LatestCtxHeight int64 = -1

func GetBlockNumberByNrOrHash(ctx context.Context, tmClient rpcclient.Client, blockNrOrHash rpc.BlockNumberOrHash) (*int64, error) {
	if blockNrOrHash.BlockHash != nil {
		res, err := blockByHash(ctx, tmClient, blockNrOrHash.BlockHash[:])
		if err != nil {
			return nil, err
		}
		return &res.Block.Height, nil
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

func recordMetrics(apiMethod string, connectionType ConnectionType, startTime time.Time, success bool) {
	metrics.IncrementRpcRequestCounter(apiMethod, string(connectionType), success)
	metrics.MeasureRpcRequestLatency(apiMethod, string(connectionType), startTime)
}

func recordMetricsWithError(apiMethod string, connectionType ConnectionType, startTime time.Time, err error) {
	metrics.IncrementErrorMetrics(apiMethod, err)
	recordMetrics(apiMethod, connectionType, startTime, err == nil)
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

func getTxHashesFromBlock(block *coretypes.ResultBlock, txConfig client.TxConfig, shouldIncludeSynthetic bool) []common.Hash {
	txHashes := []common.Hash{}
	for i, tx := range block.Block.Data.Txs {
		sdkTx, err := txConfig.TxDecoder()(tx)
		if err != nil {
			fmt.Printf("error decoding tx %d in block %d, skipping\n", i, block.Block.Height)
			continue
		}
		if len(sdkTx.GetMsgs()) > 0 {
			if evmTx, ok := sdkTx.GetMsgs()[0].(*types.MsgEVMTransaction); ok {
				if evmTx.IsAssociateTx() {
					continue
				}
				ethtx, _ := evmTx.AsTransaction()
				txHashes = append(txHashes, ethtx.Hash())
			}
		}
		if shouldIncludeSynthetic {
			txHashes = append(txHashes, sha256.Sum256(tx))
		}
	}
	return txHashes
}

func isReceiptFromAnteError(receipt *types.Receipt) bool {
	// hacky heuristic
	return receipt.EffectiveGasPrice == 0
}

type ParallelRunner struct {
	Done  sync.WaitGroup
	Queue chan func()
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
				f()
			}
		}()
	}
	return pr
}

func recoverAndLog() {
	if e := recover(); e != nil {
		fmt.Printf("Panic recovered: %s\n", e)
		debug.PrintStack()
	}
}

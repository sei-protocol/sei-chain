package evmrpc

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

const LatestCtxHeight int64 = -1

// Since we use a static base fee, we want GasUsedRatio returned in RPC queries
// to reflect the fact that base fee would never change, which is only true if
// the block is exactly half-utilized.
const GasUsedRatio float64 = 0.5

type RPCTransaction struct {
	BlockHash           *common.Hash         `json:"blockHash"`
	BlockNumber         *hexutil.Big         `json:"blockNumber"`
	From                common.Address       `json:"from"`
	Gas                 hexutil.Uint64       `json:"gas"`
	GasPrice            *hexutil.Big         `json:"gasPrice"`
	GasFeeCap           *hexutil.Big         `json:"maxFeePerGas,omitempty"`
	GasTipCap           *hexutil.Big         `json:"maxPriorityFeePerGas,omitempty"`
	MaxFeePerBlobGas    *hexutil.Big         `json:"maxFeePerBlobGas,omitempty"`
	Hash                common.Hash          `json:"hash"`
	Input               hexutil.Bytes        `json:"input"`
	Nonce               hexutil.Uint64       `json:"nonce"`
	To                  *common.Address      `json:"to"`
	TransactionIndex    *hexutil.Uint64      `json:"transactionIndex"`
	Value               *hexutil.Big         `json:"value"`
	Type                hexutil.Uint64       `json:"type"`
	Accesses            *ethtypes.AccessList `json:"accessList,omitempty"`
	ChainID             *hexutil.Big         `json:"chainId,omitempty"`
	BlobVersionedHashes []common.Hash        `json:"blobVersionedHashes,omitempty"`
	V                   *hexutil.Big         `json:"v"`
	R                   *hexutil.Big         `json:"r"`
	S                   *hexutil.Big         `json:"s"`
	YParity             *hexutil.Uint64      `json:"yParity,omitempty"`
}

func hydrateTransaction(
	tx *ethtypes.Transaction,
	blocknumber *big.Int,
	blockhash common.Hash,
	receipt *types.Receipt,
) RPCTransaction {
	idx := hexutil.Uint64(receipt.TransactionIndex)
	al := tx.AccessList()
	v, r, s := tx.RawSignatureValues()
	v = ante.AdjustV(v, tx.Type(), tx.ChainId())
	var yparity *hexutil.Uint64
	if tx.Type() != ethtypes.LegacyTxType {
		yp := hexutil.Uint64(v.Sign())
		yparity = &yp
	}
	return RPCTransaction{
		BlockHash:           &blockhash,
		BlockNumber:         (*hexutil.Big)(blocknumber),
		From:                common.HexToAddress(receipt.From),
		Gas:                 hexutil.Uint64(tx.Gas()),
		GasPrice:            (*hexutil.Big)(tx.GasPrice()),
		GasFeeCap:           (*hexutil.Big)(tx.GasFeeCap()),
		GasTipCap:           (*hexutil.Big)(tx.GasTipCap()),
		MaxFeePerBlobGas:    (*hexutil.Big)(tx.BlobGasFeeCap()),
		Hash:                tx.Hash(),
		Input:               tx.Data(),
		Nonce:               hexutil.Uint64(tx.Nonce()),
		To:                  tx.To(),
		TransactionIndex:    &idx,
		Value:               (*hexutil.Big)(tx.Value()),
		Accesses:            &al,
		ChainID:             (*hexutil.Big)(tx.ChainId()),
		BlobVersionedHashes: tx.BlobHashes(),
		V:                   (*hexutil.Big)(v),
		S:                   (*hexutil.Big)(s),
		R:                   (*hexutil.Big)(r),
		YParity:             yparity,
	}
}

func GetBlockNumberByNrOrHash(ctx context.Context, tmClient rpcclient.Client, blockNrOrHash rpc.BlockNumberOrHash) (*int64, error) {
	if blockNrOrHash.BlockHash != nil {
		res, err := tmClient.BlockByHash(ctx, blockNrOrHash.BlockHash[:])
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
		numberPtr = &genesisRes.Genesis.InitialHeight
	default:
		numberI64 := number.Int64()
		numberPtr = &numberI64
	}
	return numberPtr, nil
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

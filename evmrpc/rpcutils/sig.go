package rpcutils

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

var signerMap = map[derived.SignerVersion]func(*big.Int) ethtypes.Signer{
	derived.London: ethtypes.NewLondonSigner,
	derived.Cancun: ethtypes.NewCancunSigner,
	derived.Prague: ethtypes.NewPragueSigner,
}

// RecoverEVMSender recovers the sender address from an Ethereum transaction
// using the same logic as the preprocess ante handler.
// This ensures consistency between transaction preprocessing and RPC queries.
func RecoverEVMSender(ethTx *ethtypes.Transaction, blockHeight int64, blockTime int64) (common.Address, error) {
	// Get the chain ID from the transaction
	chainID := ethTx.ChainId()

	// Get the chain config and determine the signer version
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)

	// Create the signer with the transaction's chain ID
	var signer ethtypes.Signer
	if chainID.Int64() == 0 {
		signer = ethtypes.NewEIP155Signer(chainID)
	} else {
		var uintBlockTime uint64
		if blockTime > 0 {
			uintBlockTime = uint64(blockTime)
		}
		version := getSignerVersion(blockHeight, uintBlockTime, ethCfg)
		signer = signerMap[version](chainID)
	}

	// Get raw signature values
	V, R, S := ethTx.RawSignatureValues()

	// Compute the transaction hash based on whether it's protected
	var txHash common.Hash
	if ethTx.Protected() {
		// For protected transactions, adjust V and use signer hash
		V = adjustV(V, ethTx.Type(), ethCfg.ChainID)
		txHash = signer.Hash(ethTx)
	} else {
		// For unprotected transactions, use Frontier signer
		txHash = ethtypes.FrontierSigner{}.Hash(ethTx)
	}

	// Recover the sender address
	evmAddr, _, _, err := helpers.GetAddresses(V, R, S, txHash)
	if err != nil {
		return common.Address{}, err
	}

	return evmAddr, nil
}

// adjustV adjusts the V value for signature recovery based on transaction type and chain ID
func adjustV(V *big.Int, txType uint8, chainID *big.Int) *big.Int {
	// Non-legacy TX always needs to be bumped by 27
	if txType != ethtypes.LegacyTxType {
		return new(big.Int).Add(V, utils.Big27)
	}

	// Legacy TX needs to be adjusted based on chainID
	// Formula: V = V - (chainID * 2) - 8
	V = new(big.Int).Sub(V, new(big.Int).Mul(chainID, utils.Big2))
	return V.Sub(V, utils.Big8)
}

// getSignerVersion determines which signer version to use based on block height and time
func getSignerVersion(blockHeight int64, blockTime uint64, ethCfg *params.ChainConfig) derived.SignerVersion {
	blockNum := big.NewInt(blockHeight)
	switch {
	case ethCfg.IsPrague(blockNum, blockTime):
		return derived.Prague
	case ethCfg.IsCancun(blockNum, blockTime):
		return derived.Cancun
	default:
		return derived.London
	}
}

// RecoverEVMSenderWithContext is a convenience wrapper that extracts block info from context
func RecoverEVMSenderWithContext(ctx sdk.Context, ethTx *ethtypes.Transaction) (common.Address, error) {
	return RecoverEVMSender(ethTx, ctx.BlockHeight(), ctx.BlockTime().Unix())
}

package app

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"

	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	
	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
)

// validateTransactionBasic performs stateless basic validation on an EVM transaction
func validateTransactionBasic(txData ethtx.TxData, blockMaxGas int64) error {
	etx := ethtypes.NewTx(txData.AsEthereumData())

	// Check code size for contract creation
	if etx.To() == nil && len(etx.Data()) > params.MaxInitCodeSize {
		return core.ErrMaxInitCodeSizeExceeded
	}

	// Check value is non-negative
	if etx.Value().Sign() < 0 {
		return sdkerrors.ErrInvalidCoins
	}

	// Calculate and validate intrinsic gas
	intrGas, err := core.IntrinsicGas(etx.Data(), etx.AccessList(), etx.SetCodeAuthorizations(), etx.To() == nil, true, true, true)
	if err != nil {
		return err
	}
	if etx.Gas() < intrGas {
		return core.ErrIntrinsicGas
	}

	// Reject BlobTxType
	if etx.Type() == ethtypes.BlobTxType {
		return sdkerrors.ErrUnsupportedTxType
	}

	// Check if gas exceeds block max gas limit
	if blockMaxGas > 0 && etx.Gas() > uint64(blockMaxGas) {
		return sdkerrors.Wrapf(sdkerrors.ErrOutOfGas, "tx gas limit %d exceeds block max gas %d", etx.Gas(), blockMaxGas)
	}

	return nil
}

// validateFeeCaps performs stateless fee cap validation on an EVM transaction
func validateFeeCaps(txData ethtx.TxData, baseFee, minFee *big.Int) error {
	if txData.GetGasFeeCap().Cmp(baseFee) < 0 {
		return sdkerrors.ErrInsufficientFee
	}
	if txData.GetGasFeeCap().Cmp(minFee) < 0 {
		return sdkerrors.ErrInsufficientFee
	}
	if txData.GetGasTipCap().Sign() < 0 {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "gas tip cap cannot be negative")
	}
	return nil
}

// validateChainID validates the chain ID for an EVM transaction
func validateChainID(txData ethtx.TxData, chainID *big.Int) error {
	txChainID := txData.GetChainID()
	if txChainID == nil {
		return sdkerrors.ErrInvalidChainID
	}

	etx := ethtypes.NewTx(txData.AsEthereumData())
	switch etx.Type() {
	case ethtypes.LegacyTxType:
		// Legacy transactions can have zero or correct chain ID
		if txChainID.Cmp(big.NewInt(0)) != 0 && txChainID.Cmp(chainID) != 0 {
			return sdkerrors.ErrInvalidChainID
		}
	default:
		// All other transaction types must have correct chain ID
		if txChainID.Cmp(chainID) != 0 {
			return sdkerrors.ErrInvalidChainID
		}
	}

	return nil
}

// calculateEffectiveGasPrice calculates the effective gas price for an EVM transaction
func calculateEffectiveGasPrice(txData ethtx.TxData, baseFee *big.Int) *big.Int {
	gasPrice := txData.EffectiveGasPrice(baseFee)
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	return gasPrice
}

// calculatePriority calculates the priority for an EVM transaction
func calculatePriority(effectiveGasPrice *big.Int, normalizer sdk.Dec) int64 {
	priority := sdk.NewDecFromBigInt(effectiveGasPrice).Quo(normalizer).TruncateInt().Int64()
	// Cap priority at max value if needed
	if priority > 1000000 { // Assuming MaxPriority constant exists
		priority = 1000000
	}
	return priority
}

// buildEVMMessage constructs a core.Message from an EVM transaction
func buildEVMMessage(tx *ethtypes.Transaction, sender []byte, baseFee *big.Int) *core.Message {
	msg := &core.Message{
		Nonce:                 tx.Nonce(),
		GasLimit:              tx.Gas(),
		GasPrice:              new(big.Int).Set(tx.GasPrice()),
		GasFeeCap:             new(big.Int).Set(tx.GasFeeCap()),
		GasTipCap:             new(big.Int).Set(tx.GasTipCap()),
		To:                    tx.To(),
		Value:                 tx.Value(),
		Data:                  tx.Data(),
		AccessList:            tx.AccessList(),
		BlobHashes:            tx.BlobHashes(),
		BlobGasFeeCap:        tx.BlobGasFeeCap(),
		SetCodeAuthorizations: tx.SetCodeAuthorizations(),
		From:                  common.BytesToAddress(sender),
	}

	// If baseFee provided, set gasPrice to effectiveGasPrice
	if baseFee != nil {
		msg.GasPrice = new(big.Int).Add(msg.GasTipCap, baseFee)
		if msg.GasPrice.Cmp(msg.GasFeeCap) > 0 {
			msg.GasPrice = msg.GasFeeCap
		}
	}

	return msg
}

// identifyTransactionType determines if a transaction is EVM or COSMOS
func identifyTransactionType(tx sdk.Tx) (pipelinetypes.TransactionType, error) {
	// Try to get EVM transaction message
	if isEVM, _ := evmante.IsEVMMessage(tx); isEVM {
		return pipelinetypes.TransactionTypeEVM, nil
	}
	return pipelinetypes.TransactionTypeCOSMOS, nil
}

// PreprocessBlock preprocesses a block, performing all stateless validations
func PreprocessBlock(
	ctx sdk.Context,
	req pipelinetypes.BlockProcessRequest,
	lastCommit abci.LastCommitInfo,
	txs [][]byte,
	txDecoder func([]byte) (sdk.Tx, error),
	helper pipelinetypes.PreprocessorHelper,
) (*pipelinetypes.PreprocessedBlock, error) {
	// Get block-level info
	baseFee := helper.GetBaseFee(ctx)
	minFee := helper.GetMinimumFeePerGas(ctx).TruncateInt().BigInt()
	chainID := helper.ChainID(ctx)
	normalizer := helper.GetPriorityNormalizer(ctx)
	consensusParamsProto := ctx.ConsensusParams()
	blockMaxGas := int64(0)
	if consensusParamsProto != nil && consensusParamsProto.Block != nil {
		blockMaxGas = consensusParamsProto.Block.MaxGas
	}
	chainConfig := evmtypes.DefaultChainConfig().EthereumConfig(chainID)

	// Decode transactions
	typedTxs := make([]sdk.Tx, len(txs))
	for i, txBytes := range txs {
		typedTx, err := txDecoder(txBytes)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error decoding transaction at index %d due to %s", i, err))
			typedTxs[i] = nil
			continue
		}
		typedTxs[i] = typedTx
	}

	// Preprocess each transaction
	preprocessedTxs := make([]*pipelinetypes.PreprocessedTx, 0, len(txs))
	for i, typedTx := range typedTxs {
		if typedTx == nil {
			continue
		}

		txType, err := identifyTransactionType(typedTx)
		if err != nil {
			return nil, err
		}

		preprocessedTx := &pipelinetypes.PreprocessedTx{
			Type:    txType,
			TxIndex: i,
		}

		if txType == pipelinetypes.TransactionTypeEVM {
			// EVM transaction preprocessing
			msg := evmtypes.MustGetEVMTransactionMessage(typedTx)
			
			// Preprocess signature recovery (already done in DecodeTransactionsConcurrently, but ensure it's done)
			if msg.Derived == nil {
				if err := evmante.Preprocess(ctx, msg, chainID, helper.EthBlockTestConfigEnabled()); err != nil {
					return nil, fmt.Errorf("error preprocessing EVM tx at index %d: %w", i, err)
				}
			}

			// Get transaction data
			txData, err := evmtypes.UnpackTxData(msg.Data)
			if err != nil {
				return nil, fmt.Errorf("error unpacking tx data at index %d: %w", i, err)
			}

			etx, _ := msg.AsTransaction()

			// Validate basic fields
			if err := validateTransactionBasic(txData, blockMaxGas); err != nil {
				return nil, fmt.Errorf("basic validation failed for tx at index %d: %w", i, err)
			}

			// Validate fee caps
			if err := validateFeeCaps(txData, baseFee, minFee); err != nil {
				return nil, fmt.Errorf("fee cap validation failed for tx at index %d: %w", i, err)
			}

			// Validate chain ID
			if err := validateChainID(txData, chainID); err != nil {
				return nil, fmt.Errorf("chain ID validation failed for tx at index %d: %w", i, err)
			}

			// Calculate effective gas price and priority
			effectiveGasPrice := calculateEffectiveGasPrice(txData, baseFee)
			priority := calculatePriority(effectiveGasPrice, normalizer)

			// Build EVM message
			evmMsg := buildEVMMessage(etx, msg.Derived.SenderEVMAddr.Bytes(), baseFee)

			// Calculate intrinsic gas
			intrGas, err := core.IntrinsicGas(etx.Data(), etx.AccessList(), etx.SetCodeAuthorizations(), etx.To() == nil, true, true, true)
			if err != nil {
				return nil, fmt.Errorf("error calculating intrinsic gas for tx at index %d: %w", i, err)
			}

			// Populate EVM-specific fields
			preprocessedTx.TxData = txData
			preprocessedTx.SenderEVMAddr = msg.Derived.SenderEVMAddr.Bytes()
			preprocessedTx.SenderSeiAddr = msg.Derived.SenderSeiAddr
			preprocessedTx.EVMMessage = evmMsg
			preprocessedTx.CodeSizeOK = true
			preprocessedTx.IntrinsicGas = intrGas
			preprocessedTx.FeeCapsOK = true
			preprocessedTx.ChainIDOK = true
			preprocessedTx.Priority = priority
			preprocessedTx.EffectiveGasPrice = effectiveGasPrice
		} else {
			// COSMOS transaction - store as-is
			preprocessedTx.CosmosTx = typedTx
		}

		preprocessedTxs = append(preprocessedTxs, preprocessedTx)
	}

	// Convert consensus params to proto format
	var consensusParams *tmproto.ConsensusParams
	if consensusParamsProto != nil {
		consensusParams = &tmproto.ConsensusParams{
			Block:     consensusParamsProto.Block,
			Evidence:  consensusParamsProto.Evidence,
			Validator: consensusParamsProto.Validator,
			Version:   consensusParamsProto.Version,
		}
	}
	
	return &pipelinetypes.PreprocessedBlock{
		Height:            req.GetHeight(),
		Hash:              req.GetHash(),
		Time:              req.GetTime(),
		ByzantineValidators: req.GetByzantineValidators(),
		LastCommit:        lastCommit,
		PreprocessedTxs:   preprocessedTxs,
		BaseFee:           baseFee,
		ChainConfig:       chainConfig,
		ConsensusParams:   consensusParams,
		BlockMaxGas:       blockMaxGas,
		Ctx:               ctx,
		Txs:               txs,
	}, nil
}


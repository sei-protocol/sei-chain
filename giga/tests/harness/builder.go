package harness

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// BuildTransaction creates an Ethereum transaction from state test data
// Note: Rebuilds the transaction with Sei's chain ID since fixtures use chain ID 1
func BuildTransaction(st *StateTestJSON, subtest StateTestPost) (*ethtypes.Transaction, common.Address, error) {
	// Get the private key
	privateKey, err := crypto.ToECDSA(st.Transaction.PrivateKey)
	if err != nil {
		return nil, common.Address{}, err
	}

	// Use sender from transaction if available, otherwise derive from key
	var sender common.Address
	if st.Transaction.Sender != (common.Address{}) {
		sender = st.Transaction.Sender
	} else {
		sender = crypto.PubkeyToAddress(privateKey.PublicKey)
	}

	// Get indexed values
	dataIdx := subtest.Indexes.Data
	gasIdx := subtest.Indexes.Gas
	valueIdx := subtest.Indexes.Value

	// Parse data
	var data []byte
	if dataIdx < len(st.Transaction.Data) {
		dataHex := st.Transaction.Data[dataIdx]
		if dataHex != "" {
			data = common.FromHex(dataHex)
		}
	}

	// Parse gas limit
	var gasLimit uint64 = 21000
	if gasIdx < len(st.Transaction.GasLimit) {
		gasLimit = parseHexUint64(st.Transaction.GasLimit[gasIdx])
	}

	// Parse value
	value := new(big.Int)
	if valueIdx < len(st.Transaction.Value) {
		value = ParseHexBig(st.Transaction.Value[valueIdx])
	}

	// Parse to address
	var to *common.Address
	if st.Transaction.To != "" {
		addr := common.HexToAddress(st.Transaction.To)
		to = &addr
	}

	// Parse nonce
	nonce := parseHexUint64(st.Transaction.Nonce)

	// Parse gas prices
	gasPrice := ParseHexBig(st.Transaction.GasPrice)
	maxFeePerGas := ParseHexBig(st.Transaction.MaxFeePerGas)
	maxPriorityFeePerGas := ParseHexBig(st.Transaction.MaxPriorityFeePerGas)

	// Determine transaction type and create accordingly
	var tx *ethtypes.Transaction

	if maxFeePerGas.Sign() > 0 || st.Transaction.MaxFeePerGas != "" {
		// EIP-1559 transaction
		var accessList ethtypes.AccessList
		if subtest.Indexes.Data < len(st.Transaction.AccessLists) && st.Transaction.AccessLists[subtest.Indexes.Data] != nil {
			accessList = *st.Transaction.AccessLists[subtest.Indexes.Data]
		}

		tx = ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			ChainID:    big.NewInt(config.DefaultChainID), // Use Sei's chain ID
			Nonce:      nonce,
			GasTipCap:  maxPriorityFeePerGas,
			GasFeeCap:  maxFeePerGas,
			Gas:        gasLimit,
			To:         to,
			Value:      value,
			Data:       data,
			AccessList: accessList,
		})
	} else if len(st.Transaction.AccessLists) > 0 {
		// EIP-2930 transaction
		var accessList ethtypes.AccessList
		if subtest.Indexes.Data < len(st.Transaction.AccessLists) && st.Transaction.AccessLists[subtest.Indexes.Data] != nil {
			accessList = *st.Transaction.AccessLists[subtest.Indexes.Data]
		}

		tx = ethtypes.NewTx(&ethtypes.AccessListTx{
			ChainID:    big.NewInt(config.DefaultChainID),
			Nonce:      nonce,
			GasPrice:   gasPrice,
			Gas:        gasLimit,
			To:         to,
			Value:      value,
			Data:       data,
			AccessList: accessList,
		})
	} else {
		// Legacy transaction
		tx = ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			Gas:      gasLimit,
			To:       to,
			Value:    value,
			Data:     data,
		})
	}

	// Sign the transaction with Sei's chain ID
	signer := ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID))
	signedTx, err := ethtypes.SignTx(tx, signer, privateKey)
	if err != nil {
		return nil, common.Address{}, err
	}

	return signedTx, sender, nil
}

// EncodeTxForApp encodes a signed transaction for the Sei app
func EncodeTxForApp(signedTx *ethtypes.Transaction) ([]byte, error) {
	tc := app.MakeEncodingConfig().TxConfig

	txData, err := ethtx.NewTxDataFromTx(signedTx)
	if err != nil {
		return nil, err
	}

	msg, err := types.NewMsgEVMTransaction(txData)
	if err != nil {
		return nil, err
	}

	txBuilder := tc.NewTxBuilder()
	err = txBuilder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}
	txBuilder.SetGasLimit(10000000000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000000))))

	txBytes, err := tc.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}

	return txBytes, nil
}

// ParseHexBig parses a hex string (with possible leading zeros) to *big.Int
func ParseHexBig(s string) *big.Int {
	if s == "" {
		return new(big.Int)
	}
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	result, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic(fmt.Sprintf("ParseHexBig: failed to parse hex string %q", s))
	}
	return result
}

// parseHexUint64 parses a hex string to uint64
func parseHexUint64(s string) uint64 {
	return ParseHexBig(s).Uint64()
}

// ============================================================================
// BlockchainTests Transaction Builder
// ============================================================================

// DecodeBlockTransactions decodes transactions from a block's RLP-encoded data.
// The RLP field contains the complete encoded block including transactions.
// Returns the decoded transactions with their senders.
func DecodeBlockTransactions(blockRLP string) ([]*ethtypes.Transaction, []common.Address, error) {
	rlpBytes := common.FromHex(blockRLP)
	if len(rlpBytes) == 0 {
		return nil, nil, fmt.Errorf("empty block RLP")
	}

	// Decode the block from RLP
	var block ethtypes.Block
	if err := rlp.DecodeBytes(rlpBytes, &block); err != nil {
		return nil, nil, fmt.Errorf("failed to decode block RLP: %w", err)
	}

	txs := block.Transactions()
	senders := make([]common.Address, len(txs))

	// Extract senders from each transaction
	for i, tx := range txs {
		// Use the appropriate signer based on transaction type
		var signer ethtypes.Signer
		if tx.Type() == ethtypes.LegacyTxType {
			// For legacy transactions, we need to use HomesteadSigner for chain ID 1
			// or the appropriate signer for the chain
			signer = ethtypes.NewEIP155Signer(tx.ChainId())
			if tx.ChainId() == nil || tx.ChainId().Sign() == 0 {
				signer = ethtypes.HomesteadSigner{}
			}
		} else {
			signer = ethtypes.LatestSignerForChainID(tx.ChainId())
		}

		sender, err := ethtypes.Sender(signer, tx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to recover sender for tx %d: %w", i, err)
		}
		senders[i] = sender
	}

	return txs, senders, nil
}

// ResignBlockTransactionsForSei takes transactions decoded from BlockchainTests (signed with chain ID 1)
// and re-signs them with Sei's chain ID. This is necessary because the test fixtures use chain ID 1
// but Sei uses a different chain ID.
func ResignBlockTransactionsForSei(txs []*ethtypes.Transaction, senders []common.Address) ([]*ethtypes.Transaction, error) {
	seiChainID := big.NewInt(config.DefaultChainID)
	seiSigner := ethtypes.LatestSignerForChainID(seiChainID)
	resignedTxs := make([]*ethtypes.Transaction, len(txs))

	for i, tx := range txs {
		// For re-signing, we need the original transaction's private key
		// But we don't have it - the transactions are pre-signed
		// So we need a different approach: rebuild the tx with the same params and Sei's chain ID

		// Get original signer to recover the sender
		var originalSigner ethtypes.Signer
		if tx.Type() == ethtypes.LegacyTxType {
			if tx.ChainId() == nil || tx.ChainId().Sign() == 0 {
				originalSigner = ethtypes.HomesteadSigner{}
			} else {
				originalSigner = ethtypes.NewEIP155Signer(tx.ChainId())
			}
		} else {
			originalSigner = ethtypes.LatestSignerForChainID(tx.ChainId())
		}

		sender, err := ethtypes.Sender(originalSigner, tx)
		if err != nil {
			return nil, fmt.Errorf("failed to recover sender for tx %d: %w", i, err)
		}

		// Verify sender matches expected
		if sender != senders[i] {
			return nil, fmt.Errorf("sender mismatch for tx %d: expected %s, got %s", i, senders[i].Hex(), sender.Hex())
		}

		// For BlockchainTests, the transactions are pre-signed with known test private keys
		// We need to look up the private key for the sender address
		privateKey, err := getTestPrivateKey(sender)
		if err != nil {
			return nil, fmt.Errorf("failed to get private key for sender %s: %w", sender.Hex(), err)
		}

		// Rebuild and re-sign the transaction with Sei's chain ID
		var newTx *ethtypes.Transaction
		switch tx.Type() {
		case ethtypes.LegacyTxType:
			newTx = ethtypes.NewTx(&ethtypes.LegacyTx{
				Nonce:    tx.Nonce(),
				GasPrice: tx.GasPrice(),
				Gas:      tx.Gas(),
				To:       tx.To(),
				Value:    tx.Value(),
				Data:     tx.Data(),
			})
		case ethtypes.AccessListTxType:
			newTx = ethtypes.NewTx(&ethtypes.AccessListTx{
				ChainID:    seiChainID,
				Nonce:      tx.Nonce(),
				GasPrice:   tx.GasPrice(),
				Gas:        tx.Gas(),
				To:         tx.To(),
				Value:      tx.Value(),
				Data:       tx.Data(),
				AccessList: tx.AccessList(),
			})
		case ethtypes.DynamicFeeTxType:
			newTx = ethtypes.NewTx(&ethtypes.DynamicFeeTx{
				ChainID:    seiChainID,
				Nonce:      tx.Nonce(),
				GasTipCap:  tx.GasTipCap(),
				GasFeeCap:  tx.GasFeeCap(),
				Gas:        tx.Gas(),
				To:         tx.To(),
				Value:      tx.Value(),
				Data:       tx.Data(),
				AccessList: tx.AccessList(),
			})
		default:
			return nil, fmt.Errorf("unsupported transaction type: %d", tx.Type())
		}

		signedTx, err := ethtypes.SignTx(newTx, seiSigner, privateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to sign tx %d: %w", i, err)
		}

		resignedTxs[i] = signedTx
	}

	return resignedTxs, nil
}

// BlockEnvFromHeader creates a StateTestEnv from a blockchain test block header
func BlockEnvFromHeader(header BlockHeader) StateTestEnv {
	return StateTestEnv{
		Coinbase:   common.HexToAddress(header.Coinbase),
		Difficulty: header.Difficulty,
		GasLimit:   header.GasLimit,
		Number:     header.Number,
		Timestamp:  header.Timestamp,
		BaseFee:    header.BaseFeePerGas,
		Random:     parseHashOrNil(header.MixHash),
	}
}

// parseHashOrNil parses a hex string to a Hash pointer, returning nil if empty
func parseHashOrNil(s string) *common.Hash {
	if s == "" || s == "0x" {
		return nil
	}
	h := common.HexToHash(s)
	return &h
}

// Known test private keys used in Ethereum tests
// These are well-known test keys used in the official Ethereum test suite
var testPrivateKeys = map[common.Address]string{
	// Primary test account used in most Ethereum tests
	common.HexToAddress("0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b"): "45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8",
	// Secondary test accounts
	common.HexToAddress("0xd02d72E067e77158444ef2020Ff2d325f929B363"): "b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291",
	common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"): "a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8a8", // Test key for EIP-1559 tests
	// System contract placeholder key
	common.HexToAddress("0x000F3df6D732807Ef1319fB7B8bB8522d0Beac02"): "0000000000000000000000000000000000000000000000000000000000000001",
	// Additional test accounts commonly used
	common.HexToAddress("0xe2AFf99a29fADcd427b47b514b42ee5394913A01"): "e8f32e723decf4051aefac8e2c93c9c5b214313817cdb01a1494b917c8436b35", // EIP-1559 intrinsic tests
	common.HexToAddress("0x2adc25665018aa1fe0e6bc666dac8fc2697ff9ba"): "0202020202020202020202020202020202020202020202020202020202020202", // Coinbase
	common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc"): "0303030303030303030303030303030303030303030303030303030303030303",
}

// getTestPrivateKey returns the private key for a known test address
func getTestPrivateKey(addr common.Address) (*ecdsa.PrivateKey, error) {
	// First check the known test keys
	if hexKey, ok := testPrivateKeys[addr]; ok {
		return crypto.HexToECDSA(hexKey)
	}

	// If we don't know this address, we can't re-sign
	return nil, fmt.Errorf("unknown test address: %s", addr.Hex())
}

// BuildBlockchainTransactionFromJSON builds a signed transaction from JSON fields.
// It first tries to use a known test private key to re-sign with Sei's chain ID.
// If the sender's key is unknown, it falls back to using the original signature.
func BuildBlockchainTransactionFromJSON(tx BlockchainTestTransaction) (*ethtypes.Transaction, common.Address, error) {
	sender := common.HexToAddress(tx.Sender)

	// Parse transaction fields
	nonce := parseHexUint64(tx.Nonce)
	gasLimit := parseHexUint64(tx.GasLimit)
	value := ParseHexBig(tx.Value)
	data := common.FromHex(tx.Data)

	var to *common.Address
	if tx.To != "" {
		addr := common.HexToAddress(tx.To)
		to = &addr
	}

	seiChainID := big.NewInt(config.DefaultChainID)

	// Try to get the private key for this sender to re-sign
	privateKey, keyErr := getTestPrivateKey(sender)
	if keyErr == nil {
		// We have the private key, re-sign with Sei chain ID
		signer := ethtypes.LatestSignerForChainID(seiChainID)
		var newTx *ethtypes.Transaction

		// Determine transaction type based on fields present
		if tx.MaxFeePerGas != "" {
			// EIP-1559 transaction
			var accessList ethtypes.AccessList
			if tx.AccessList != nil {
				accessList = *tx.AccessList
			}
			newTx = ethtypes.NewTx(&ethtypes.DynamicFeeTx{
				ChainID:    seiChainID,
				Nonce:      nonce,
				GasTipCap:  ParseHexBig(tx.MaxPriorityFeePerGas),
				GasFeeCap:  ParseHexBig(tx.MaxFeePerGas),
				Gas:        gasLimit,
				To:         to,
				Value:      value,
				Data:       data,
				AccessList: accessList,
			})
		} else if tx.AccessList != nil {
			// EIP-2930 transaction
			newTx = ethtypes.NewTx(&ethtypes.AccessListTx{
				ChainID:    seiChainID,
				Nonce:      nonce,
				GasPrice:   ParseHexBig(tx.GasPrice),
				Gas:        gasLimit,
				To:         to,
				Value:      value,
				Data:       data,
				AccessList: *tx.AccessList,
			})
		} else {
			// Legacy transaction
			newTx = ethtypes.NewTx(&ethtypes.LegacyTx{
				Nonce:    nonce,
				GasPrice: ParseHexBig(tx.GasPrice),
				Gas:      gasLimit,
				To:       to,
				Value:    value,
				Data:     data,
			})
		}

		signedTx, err := ethtypes.SignTx(newTx, signer, privateKey)
		if err != nil {
			return nil, common.Address{}, fmt.Errorf("failed to sign transaction: %w", err)
		}

		return signedTx, sender, nil
	}

	// Don't have private key - reconstruct with original signature
	// This uses the original chain ID (1) and signature from the test fixture
	r := ParseHexBig(tx.R)
	s := ParseHexBig(tx.S)
	v := ParseHexBig(tx.V)

	var signedTx *ethtypes.Transaction

	// Determine transaction type and reconstruct with original signature
	if tx.MaxFeePerGas != "" {
		// EIP-1559 transaction
		var accessList ethtypes.AccessList
		if tx.AccessList != nil {
			accessList = *tx.AccessList
		}
		signedTx = ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			ChainID:    big.NewInt(1), // Original chain ID
			Nonce:      nonce,
			GasTipCap:  ParseHexBig(tx.MaxPriorityFeePerGas),
			GasFeeCap:  ParseHexBig(tx.MaxFeePerGas),
			Gas:        gasLimit,
			To:         to,
			Value:      value,
			Data:       data,
			AccessList: accessList,
			V:          v,
			R:          r,
			S:          s,
		})
	} else if tx.AccessList != nil {
		// EIP-2930 transaction
		signedTx = ethtypes.NewTx(&ethtypes.AccessListTx{
			ChainID:    big.NewInt(1),
			Nonce:      nonce,
			GasPrice:   ParseHexBig(tx.GasPrice),
			Gas:        gasLimit,
			To:         to,
			Value:      value,
			Data:       data,
			AccessList: *tx.AccessList,
			V:          v,
			R:          r,
			S:          s,
		})
	} else {
		// Legacy transaction
		signedTx = ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    nonce,
			GasPrice: ParseHexBig(tx.GasPrice),
			Gas:      gasLimit,
			To:       to,
			Value:    value,
			Data:     data,
			V:        v,
			R:        r,
			S:        s,
		})
	}

	return signedTx, sender, nil
}

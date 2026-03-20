package harness

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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
		value = parseHexBig(st.Transaction.Value[valueIdx])
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
	gasPrice := parseHexBig(st.Transaction.GasPrice)
	maxFeePerGas := parseHexBig(st.Transaction.MaxFeePerGas)
	maxPriorityFeePerGas := parseHexBig(st.Transaction.MaxPriorityFeePerGas)

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

// parseHexBig parses a hex string (with possible leading zeros) to *big.Int
func parseHexBig(s string) *big.Int {
	if s == "" {
		return new(big.Int)
	}
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	result, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic(fmt.Sprintf("parseHexBig: failed to parse hex string %q", s))
	}
	return result
}

// parseHexUint64 parses a hex string to uint64
func parseHexUint64(s string) uint64 {
	return parseHexBig(s).Uint64()
}

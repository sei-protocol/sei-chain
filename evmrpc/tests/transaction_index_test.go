package tests

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

func TestTransactionIndex_CalculateCosmosTxIndex(t *testing.T) {
	t.Run("WithExistingCosmosTxIndex", func(t *testing.T) {
		// Setup: Create a TransactionIndex with existing cosmos tx index
		existingIndex := uint32(3)
		ti := evmrpc.NewTransactionIndexFromCosmosIndex(existingIndex)

		// Mock block and dependencies
		block := &coretypes.ResultBlock{}
		decoder := func(txBytes []byte) (sdk.Tx, error) { return nil, nil }
		receiptChecker := func(hash common.Hash) *types.Receipt { return nil }

		// Test: Should return existing index without calculation
		cosmosIndex, found := ti.CalculateCosmosTxIndex(block, decoder, receiptChecker, false)
		
		require.True(t, found)
		require.Equal(t, existingIndex, cosmosIndex)
	})

	t.Run("CalculateFromEVMTxIndex", func(t *testing.T) {
		// Setup: Create transactions
		cosmosTx1 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 0)
		tx1Data := send(0)
		signedTx1 := signTxWithMnemonic(tx1Data, mnemonic1)
		tx1 := encodeEvmTx(tx1Data, signedTx1)
		cosmosTx2 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 1)
		tx2Data := send(1)
		signedTx2 := signTxWithMnemonic(tx2Data, mnemonic1)
		tx2 := encodeEvmTx(tx2Data, signedTx2)

		// Create block with mixed transactions
		block := &coretypes.ResultBlock{
			Block: &tmtypes.Block{
				Data: tmtypes.Data{Txs: []tmtypes.Tx{cosmosTx1, tx1, cosmosTx2, tx2}},
			},
		}

		SetupTestServer([][][]byte{{cosmosTx1, tx1, cosmosTx2, tx2}}, mnemonicInitializer(mnemonic1)).Run(
			func(port int) {
				// Create TransactionIndex with EVM index 1 (should map to cosmos index 1)
				ti := evmrpc.NewTransactionIndexFromEVMIndex(1)

				decoder := func(txBytes []byte) (sdk.Tx, error) {
					return nil, nil
				}

				receiptChecker := func(hash common.Hash) *types.Receipt {
					if hash == signedTx1.Hash() || hash == signedTx2.Hash() {
						return &types.Receipt{
							TxHashHex: hash.Hex(),
							Status:    1,
						}
					}
					return nil
				}

				// Test: Calculate cosmos index from EVM index
				cosmosIndex, found := ti.CalculateCosmosTxIndex(block, decoder, receiptChecker, false)
				
				require.True(t, found)
				require.Equal(t, uint32(1), cosmosIndex) // Should be 1 because EVM index 1 is at cosmos index 1
			},
		)
	})

	t.Run("CalculateFromEVMTxIndexWithSynthetic", func(t *testing.T) {
		// Setup: Create transactions
		cosmosTx1 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 0)
		tx1Data := send(0)
		signedTx1 := signTxWithMnemonic(tx1Data, mnemonic1)
		tx1 := encodeEvmTx(tx1Data, signedTx1)
		cosmosTx2 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 1)

		// Create block with mixed transactions
		block := &coretypes.ResultBlock{
			Block: &tmtypes.Block{
				Data: tmtypes.Data{Txs: []tmtypes.Tx{cosmosTx1, tx1, cosmosTx2}},
			},
		}

		SetupTestServer([][][]byte{{cosmosTx1, tx1, cosmosTx2}}, mnemonicInitializer(mnemonic1)).Run(
			func(port int) {
				// Create TransactionIndex with EVM index 1 (should map to cosmos index 2 with synthetic)
				ti := evmrpc.NewTransactionIndexFromEVMIndex(1)

				decoder := func(txBytes []byte) (sdk.Tx, error) {
					return nil, nil
				}

				receiptChecker := func(hash common.Hash) *types.Receipt {
					// Return receipt for cosmos tx as well when including synthetic
					return &types.Receipt{
						TxHashHex: hash.Hex(),
						Status:    1,
					}
				}

				// Test: Calculate cosmos index from EVM index with synthetic included
				cosmosIndex, found := ti.CalculateCosmosTxIndex(block, decoder, receiptChecker, true)
				
				require.True(t, found)
				require.Equal(t, uint32(2), cosmosIndex) // Should be 2 because EVM index 1 with synthetic is at cosmos index 2
			},
		)
	})

	t.Run("NotFound", func(t *testing.T) {
		// Setup: Create a TransactionIndex with non-existent EVM index
		ti := evmrpc.NewTransactionIndexFromEVMIndex(999)

		block := &coretypes.ResultBlock{
			Block: &tmtypes.Block{
				Data: tmtypes.Data{Txs: []tmtypes.Tx{}},
			},
		}

		decoder := func(txBytes []byte) (sdk.Tx, error) { return nil, nil }
		receiptChecker := func(hash common.Hash) *types.Receipt { return nil }

		// Test: Should return not found
		cosmosIndex, found := ti.CalculateCosmosTxIndex(block, decoder, receiptChecker, false)
		
		require.False(t, found)
		require.Equal(t, uint32(0), cosmosIndex)
	})
}

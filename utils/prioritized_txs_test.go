package utils

import (
	"testing"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

func TestIsTxPrioritized(t *testing.T) {
	tests := []struct {
		name     string
		tx       seitypes.Tx
		expected bool
	}{
		{
			name:     "Empty transaction",
			tx:       createTestTx([]seitypes.Msg{}),
			expected: true,
		},
		{
			name: "Oracle aggregate vote message",
			tx: createTestTx([]seitypes.Msg{
				&oracletypes.MsgAggregateExchangeRateVote{
					ExchangeRates: "1.0usei,2.0uusd",
					Feeder:        "sei1abc123",
					Validator:     "seivaloper1abc123",
				},
			}),
			expected: true,
		},
		{
			name: "Oracle delegate feed consent message",
			tx: createTestTx([]seitypes.Msg{
				&oracletypes.MsgDelegateFeedConsent{
					Operator: "seivaloper1abc123",
					Delegate: "sei1abc123",
				},
			}),
			expected: true,
		},
		{
			name: "Multiple oracle messages",
			tx: createTestTx([]seitypes.Msg{
				&oracletypes.MsgAggregateExchangeRateVote{
					ExchangeRates: "1.0usei",
					Feeder:        "sei1abc123",
					Validator:     "seivaloper1abc123",
				},
				&oracletypes.MsgDelegateFeedConsent{
					Operator: "seivaloper1abc123",
					Delegate: "sei1abc123",
				},
			}),
			expected: true,
		},
		{
			name: "Bank send message (not prioritized)",
			tx: createTestTx([]seitypes.Msg{
				&banktypes.MsgSend{
					FromAddress: "sei1abc123",
					ToAddress:   "sei1def456",
					Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100)),
				},
			}),
			expected: false,
		},
		{
			name: "Mixed messages (oracle + bank)",
			tx: createTestTx([]seitypes.Msg{
				&oracletypes.MsgAggregateExchangeRateVote{
					ExchangeRates: "1.0usei",
					Feeder:        "sei1abc123",
					Validator:     "seivaloper1abc123",
				},
				&banktypes.MsgSend{
					FromAddress: "sei1abc123",
					ToAddress:   "sei1def456",
					Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100)),
				},
			}),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTxPrioritized(tt.tx)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsTxPrioritizedEdgeCases(t *testing.T) {
	// Test with transaction containing no messages
	emptyTx := createTestTx([]seitypes.Msg{})
	require.True(t, IsTxPrioritized(emptyTx))
}

// Helper function to create a test transaction with given messages
func createTestTx(msgs []seitypes.Msg) seitypes.Tx {
	return &TestTx{msgs: msgs}
}

// TestTx is a simple implementation of seitypes.Tx for testing
type TestTx struct {
	msgs []seitypes.Msg
}

func (tx *TestTx) GetMsgs() []seitypes.Msg {
	return tx.msgs
}

func (tx *TestTx) ValidateBasic() error {
	return nil
}

func (tx *TestTx) GetSigners() []seitypes.AccAddress {
	return nil
}

func (tx *TestTx) GetPubKeys() ([]cryptotypes.PubKey, error) {
	return nil, nil
}

func (tx *TestTx) GetSignaturesV2() ([]signing.SignatureV2, error) {
	return nil, nil
}

func (tx *TestTx) GetGasEstimate() uint64 {
	return 0
}

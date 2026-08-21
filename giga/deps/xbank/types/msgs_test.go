package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	bank "github.com/sei-protocol/sei-chain/giga/deps/xbank/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func TestValidateInputsOutputs(t *testing.T) {
	inputAddr := sdk.AccAddress([]byte("_______alice________"))
	outputAddr := sdk.AccAddress([]byte("________bob_________"))

	t.Run("aggregates repeated denominations", func(t *testing.T) {
		inputs := []bank.Input{
			bank.NewInput(inputAddr, sdk.NewCoins(sdk.NewInt64Coin("aaa", 1))),
			bank.NewInput(inputAddr, sdk.NewCoins(sdk.NewInt64Coin("aaa", 2))),
		}
		outputs := []bank.Output{
			bank.NewOutput(outputAddr, sdk.NewCoins(sdk.NewInt64Coin("aaa", 3))),
		}

		require.NoError(t, bank.ValidateInputsOutputs(inputs, outputs))
	})

	t.Run("rejects mismatched totals", func(t *testing.T) {
		inputs := []bank.Input{
			bank.NewInput(inputAddr, sdk.NewCoins(sdk.NewInt64Coin("aaa", 2))),
		}
		outputs := []bank.Output{
			bank.NewOutput(outputAddr, sdk.NewCoins(sdk.NewInt64Coin("aaa", 1))),
		}

		require.ErrorIs(t, bank.ValidateInputsOutputs(inputs, outputs), bank.ErrInputOutputMismatch)
	})
}

func TestValidateInputsOutputsRejectsMismatchedDenominations(t *testing.T) {
	inputAddr := sdk.AccAddress([]byte("_______alice________"))
	outputAddr := sdk.AccAddress([]byte("________bob_________"))

	for _, tc := range []struct {
		name        string
		inputDenom  string
		outputDenom string
	}{
		{name: "input denomination missing from outputs", inputDenom: "aaa", outputDenom: "bbb"},
		{name: "output denomination missing from inputs", inputDenom: "bbb", outputDenom: "aaa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := []bank.Input{
				bank.NewInput(inputAddr, sdk.NewCoins(sdk.NewInt64Coin(tc.inputDenom, 1))),
			}
			outputs := []bank.Output{
				bank.NewOutput(outputAddr, sdk.NewCoins(sdk.NewInt64Coin(tc.outputDenom, 1))),
			}

			require.ErrorIs(t, bank.ValidateInputsOutputs(inputs, outputs), bank.ErrInputOutputMismatch)
		})
	}
}

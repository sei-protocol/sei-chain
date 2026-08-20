package types

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// ValidateBasic - validate transaction input
func (in Input) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(in.Address)
	if err != nil {
		return err
	}

	if !in.Coins.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, in.Coins.String())
	}

	if !in.Coins.IsAllPositive() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, in.Coins.String())
	}

	return nil
}

// NewInput - create a transaction input, used with MsgMultiSend
func NewInput(addr sdk.AccAddress, coins sdk.Coins) Input {
	return Input{
		Address: addr.String(),
		Coins:   coins,
	}
}

// ValidateBasic - validate transaction output
func (out Output) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(out.Address)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid output address (%s)", err)
	}

	if !out.Coins.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, out.Coins.String())
	}

	if !out.Coins.IsAllPositive() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, out.Coins.String())
	}

	return nil
}

// NewOutput - create a transaction output, used with MsgMultiSend
func NewOutput(addr sdk.AccAddress, coins sdk.Coins) Output {
	return Output{
		Address: addr.String(),
		Coins:   coins,
	}
}

// ValidateInputsOutputs validates that each respective input and output is
// valid and that the sum of inputs is equal to the sum of outputs.
func ValidateInputsOutputs(inputs []Input, outputs []Output) error {
	inCoinsCount := 0
	for _, in := range inputs {
		inCoinsCount += len(in.Coins)
	}

	inCoinsMap := make(map[string]sdk.Int, inCoinsCount)

	for _, in := range inputs {
		if err := in.ValidateBasic(); err != nil {
			return err
		}
		addCoins(inCoinsMap, in.Coins)
	}

	outCoinsMap := make(map[string]sdk.Int, len(inCoinsMap))

	for _, out := range outputs {
		if err := out.ValidateBasic(); err != nil {
			return err
		}
		addCoins(outCoinsMap, out.Coins)
	}

	totalIn := coinsFromTotals(inCoinsMap)
	totalOut := coinsFromTotals(outCoinsMap)

	// Preserve Coins.IsEqual's panic for equal-length sets with different denominations.
	if !totalIn.IsEqual(totalOut) {
		return ErrInputOutputMismatch
	}
	return nil
}

// coinsFromTotals returns a sorted coin set containing the aggregated totals.
func coinsFromTotals(coinsMap map[string]sdk.Int) sdk.Coins {
	result := make(sdk.Coins, 0, len(coinsMap))
	for denom, amount := range coinsMap {
		result = append(result, sdk.NewCoin(denom, amount))
	}
	return result.Sort()
}

// addCoins aggregates coins by denomination.
func addCoins(totals map[string]sdk.Int, coins sdk.Coins) {
	for _, coin := range coins {
		amount, exists := totals[coin.Denom]
		if exists {
			totals[coin.Denom] = amount.Add(coin.Amount)
		} else {
			// A missing entry contains an sdk.Int with a nil big.Int, which cannot be added to.
			totals[coin.Denom] = coin.Amount
		}
	}
}

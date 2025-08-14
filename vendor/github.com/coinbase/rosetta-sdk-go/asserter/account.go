// Copyright 2020 Coinbase, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package asserter

import (
	"fmt"

	"github.com/coinbase/rosetta-sdk-go/types"
)

// ContainsDuplicateCurrency retruns a boolean indicating
// if an array of *types.Currency contains any duplicate currencies.
func ContainsDuplicateCurrency(currencies []*types.Currency) *types.Currency {
	seen := map[string]struct{}{}
	for _, curr := range currencies {
		key := types.Hash(curr)
		if _, ok := seen[key]; ok {
			return curr
		}

		seen[key] = struct{}{}
	}

	return nil
}

// ContainsCurrency returns a boolean indicating if a
// *types.Currency is contained within a slice of
// *types.Currency. The check for equality takes
// into account everything within the types.Currency
// struct (including currency.Metadata).
func ContainsCurrency(currencies []*types.Currency, currency *types.Currency) bool {
	for _, curr := range currencies {
		if types.Hash(curr) == types.Hash(currency) {
			return true
		}
	}

	return false
}

// AssertUniqueAmounts returns an error if a slice
// of types.Amount is invalid. It is considered invalid if the same
// currency is returned multiple times (these shoould be
// consolidated) or if a types.Amount is considered invalid.
func AssertUniqueAmounts(amounts []*types.Amount) error {
	seen := map[string]struct{}{}
	for _, amount := range amounts {
		// Ensure a currency is used at most once
		key := types.Hash(amount.Currency)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("currency %+v used multiple times", amount.Currency)
		}
		seen[key] = struct{}{}

		// Check amount for validity
		if err := Amount(amount); err != nil {
			return err
		}
	}

	return nil
}

// AccountBalanceResponse returns an error if the provided
// types.BlockIdentifier is invalid, if the requestBlock
// is not nil and not equal to the response block, or
// if the same currency is present in multiple amounts.
func AccountBalanceResponse(
	requestBlock *types.PartialBlockIdentifier,
	response *types.AccountBalanceResponse,
) error {
	if err := BlockIdentifier(response.BlockIdentifier); err != nil {
		return fmt.Errorf("%w: block identifier is invalid", err)
	}

	if err := AssertUniqueAmounts(response.Balances); err != nil {
		return fmt.Errorf("%w: balance amounts are invalid", err)
	}

	if requestBlock == nil {
		return nil
	}

	if requestBlock.Hash != nil && *requestBlock.Hash != response.BlockIdentifier.Hash {
		return fmt.Errorf(
			"%w: requested block hash %s but got %s",
			ErrReturnedBlockHashMismatch,
			*requestBlock.Hash,
			response.BlockIdentifier.Hash,
		)
	}

	if requestBlock.Index != nil && *requestBlock.Index != response.BlockIdentifier.Index {
		return fmt.Errorf(
			"%w: requested block index %d but got %d",
			ErrReturnedBlockIndexMismatch,
			*requestBlock.Index,
			response.BlockIdentifier.Index,
		)
	}

	return nil
}

// AccountCoinsResponse returns an error if the provided
// *types.AccountCoinsResponse is invalid.
func AccountCoinsResponse(
	response *types.AccountCoinsResponse,
) error {
	if err := BlockIdentifier(response.BlockIdentifier); err != nil {
		return fmt.Errorf("%w: block identifier is invalid", err)
	}

	if err := Coins(response.Coins); err != nil {
		return fmt.Errorf("%w: coins are invalid", err)
	}

	return nil
}

package staking

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

const escrowAddressSeed = "sei/evmonly/staking/escrow/v1"

var escrowAddress = common.BytesToAddress(crypto.Keccak256([]byte(escrowAddressSeed))[12:])

// EscrowAddress is the module-account-like address that holds bonded stake.
func EscrowAddress() common.Address {
	return escrowAddress
}

func transferPrecompileValueToEscrow(ctx *precompiles.Context) error {
	return transferNativeValue(ctx, ctx.Address, escrowAddress, ctx.ApparentValue)
}

func transferStakeFromEscrowToAddress(balances precompiles.BalanceTransfer, delegator string, amount *big.Int) error {
	if !common.IsHexAddress(delegator) {
		return fmt.Errorf("delegator address %q is not an EVM address", delegator)
	}
	return transferNativeValueWithBalances(balances, escrowAddress, common.HexToAddress(delegator), sweiFromUsei(amount))
}

func transferNativeValue(ctx *precompiles.Context, from common.Address, to common.Address, amount *big.Int) error {
	return transferNativeValueWithBalances(ctx.Balances, from, to, amount)
}

func transferNativeValueWithBalances(balances precompiles.BalanceTransfer, from common.Address, to common.Address, amount *big.Int) error {
	if amount == nil || amount.Sign() == 0 {
		return nil
	}
	if balances == nil {
		return errMissingBalanceTransfer
	}
	return balances.Transfer(from, to, amount)
}

func sweiFromUsei(amount *big.Int) *big.Int {
	if amount == nil {
		return new(big.Int)
	}
	return new(big.Int).Mul(amount, useiToSwei)
}

package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

const (
	evmModule = "evm"
	baseDenom = "usei"
)

func TestCode(t *testing.T) {
	k := &keeper.EVMTestApp.EvmKeeper
	ctx := keeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	_, addr := keeper.MockAddressPair()

	// Untouched address: code hash is the zero hash, not EmptyCodeHash.
	require.Equal(t, common.Hash{}, k.GetCodeHash(ctx, addr))

	// Funding the address creates the underlying account, after which the
	// code hash becomes EmptyCodeHash (account exists, has no code).
	oneUsei := sdk.NewCoins(sdk.NewCoin(baseDenom, sdk.OneInt()))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, evmModule, oneUsei))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmModule, addr[:], oneUsei))

	require.Equal(t, ethtypes.EmptyCodeHash, k.GetCodeHash(ctx, addr))
	require.Nil(t, k.GetCode(ctx, addr))
	require.Equal(t, 0, k.GetCodeSize(ctx, addr))

	// After SetCode, hash, bytes, and size all reflect the new code.
	code := []byte{1, 2, 3, 4, 5}
	k.SetCode(ctx, addr, code)
	require.Equal(t, crypto.Keccak256Hash(code), k.GetCodeHash(ctx, addr))
	require.Equal(t, code, k.GetCode(ctx, addr))
	require.Equal(t, len(code), k.GetCodeSize(ctx, addr))

	// SetCode must also associate a Sei account with the EVM address.
	seiAddr := k.GetSeiAddressOrDefault(ctx, addr)
	acct := k.AccountKeeper().GetAccount(ctx, seiAddr)
	require.Equal(t, sdk.AccAddress(addr[:]), acct.GetAddress())
}

func TestCodeDelegation(t *testing.T) {
	k := &keeper.EVMTestApp.EvmKeeper
	ctx := keeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	_, addr := keeper.MockAddressPair()
	_, target := keeper.MockAddressPair()

	// EIP-7702 delegation is stored verbatim and must NOT create a
	// Sei-address mapping for the delegating account.
	code := ethtypes.AddressToDelegation(target)
	k.SetCode(ctx, addr, code)

	require.Equal(t, code, k.GetCode(ctx, addr))
	_, found := k.GetSeiAddress(ctx, addr)
	require.False(t, found)
}

func TestNilCode(t *testing.T) {
	k := &keeper.EVMTestApp.EvmKeeper
	ctx := keeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	_, addr := keeper.MockAddressPair()

	// Writing nil code stores no bytes but normalises the hash to
	// EmptyCodeHash rather than leaving it as the zero hash.
	k.SetCode(ctx, addr, nil)
	require.Nil(t, k.GetCode(ctx, addr))
	require.Equal(t, 0, k.GetCodeSize(ctx, addr))
	require.Equal(t, ethtypes.EmptyCodeHash, k.GetCodeHash(ctx, addr))
}

func TestGetCodeHashWithNonceButZeroBalance(t *testing.T) {
	k := &keeper.EVMTestApp.EvmKeeper
	ctx := keeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	_, addr := keeper.MockAddressPair()

	require.Equal(t, common.Hash{}, k.GetCodeHash(ctx, addr))

	// Bumping the nonce marks the account as existing even with a zero
	// balance, so the code hash flips from zero to EmptyCodeHash.
	k.SetNonce(ctx, addr, 1)

	require.Equal(t, ethtypes.EmptyCodeHash, k.GetCodeHash(ctx, addr))
	require.Zero(t, k.GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, addr)).Sign())
}

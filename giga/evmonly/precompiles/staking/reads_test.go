package staking

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

func TestReadValidatorStake(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	h.store().SetUint256(at(validatorBase(val1), validatorCommissionRate), dec.One())

	got, ok := ReadValidatorStake(h.state, val1)
	require.True(t, ok)
	require.Equal(t, sei(35), got.Tokens)
	require.Equal(t, dec.FromInt(sei(35)), got.DelegatorShares)
	require.Equal(t, dec.One(), got.CommissionRate)

	_, ok = ReadValidatorStake(h.state, alice)
	require.False(t, ok)
}

func TestDelegationTokens(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))

	got, ok := DelegationTokens(h.state, alice, val1)
	require.True(t, ok)
	require.Equal(t, sei(5), got)

	_, ok = DelegationTokens(h.state, alice, val2)
	require.False(t, ok, "alice does not delegate to val2")
	_, ok = DelegationTokens(h.state, alice, bob)
	require.False(t, ok, "bob has no validator")
}

func TestDelegationTokensTruncateToWei(t *testing.T) {
	h := newHarness(t, testParams)
	s := h.store()
	v, ok := s.validator(val1)
	require.True(t, ok)
	// Three shares of a validator with three times as many shares as tokens are
	// worth one wei.
	v.delegatorShares = dec.FromInt(new(big.Int).Mul(v.tokens, big.NewInt(3)))
	s.setValidator(val1, v)
	s.setDelegation(alice, val1, dec.FromInt(big.NewInt(4)))

	got, ok := DelegationTokens(h.state, alice, val1)
	require.True(t, ok)
	require.Equal(t, big.NewInt(1), got, "4/3 of a wei truncates to one")
}

func TestDelegatorValidators(t *testing.T) {
	h := newHarness(t, testParams)
	require.Empty(t, DelegatorValidators(h.state, alice))
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.delegate(alice, val2, sei(5)))
	require.Equal(t, []common.Address{val1, val2}, DelegatorValidators(h.state, alice))
	require.Equal(t, solstorage.AddressKey(val1), h.store().delegatedValidators(alice).At(0))
}

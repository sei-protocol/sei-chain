package vtype

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPAW_SetNonce_MergeOntoZeroBase(t *testing.T) {
	paw := NewPendingAccountWrite().SetNonce(42)
	base := NewAccountData()

	result := paw.Merge(base, 100)

	require.Equal(t, uint64(42), result.GetNonce())
	require.Equal(t, int64(100), result.GetBlockHeight())
	var zero [32]byte
	require.Equal(t, &zero, result.GetBalance())
	require.Equal(t, &zero, result.GetCodeHash())
}

func TestPAW_SetCodeHash_MergeOntoExistingAccount(t *testing.T) {
	base := NewAccountData().
		SetBlockHeight(50).
		SetBalance(toArray32(leftPad32([]byte{0xff}))).
		SetNonce(10).
		SetCodeHash(toArray32(bytes.Repeat([]byte{0xaa}, 32)))

	newCodeHash := toArray32(bytes.Repeat([]byte{0xbb}, 32))
	paw := NewPendingAccountWrite().SetCodeHash(newCodeHash)

	result := paw.Merge(base, 100)

	// Changed field
	require.Equal(t, newCodeHash, result.GetCodeHash())
	// Unchanged fields carried over from base
	require.Equal(t, toArray32(leftPad32([]byte{0xff})), result.GetBalance())
	require.Equal(t, uint64(10), result.GetNonce())
	// Block height updated
	require.Equal(t, int64(100), result.GetBlockHeight())
}

func TestPAW_SetBalance_MergeOntoExistingAccount(t *testing.T) {
	base := NewAccountData().
		SetBlockHeight(50).
		SetBalance(toArray32(leftPad32([]byte{0x01}))).
		SetNonce(5)

	newBalance := toArray32(leftPad32([]byte{0x02}))
	paw := NewPendingAccountWrite().SetBalance(newBalance)

	result := paw.Merge(base, 60)

	require.Equal(t, newBalance, result.GetBalance())
	require.Equal(t, uint64(5), result.GetNonce())
	require.Equal(t, int64(60), result.GetBlockHeight())
}

func TestPAW_MultipleFields(t *testing.T) {
	base := NewAccountData().
		SetBlockHeight(1).
		SetBalance(toArray32(leftPad32([]byte{0x01}))).
		SetNonce(1).
		SetCodeHash(toArray32(bytes.Repeat([]byte{0x01}, 32)))

	newBalance := toArray32(leftPad32([]byte{0x02}))
	newCodeHash := toArray32(bytes.Repeat([]byte{0x02}, 32))
	paw := NewPendingAccountWrite().
		SetBalance(newBalance).
		SetNonce(99).
		SetCodeHash(newCodeHash)

	result := paw.Merge(base, 200)

	require.Equal(t, newBalance, result.GetBalance())
	require.Equal(t, uint64(99), result.GetNonce())
	require.Equal(t, newCodeHash, result.GetCodeHash())
	require.Equal(t, int64(200), result.GetBlockHeight())
}

func TestPAW_ZeroNonce(t *testing.T) {
	base := NewAccountData().SetNonce(42)
	paw := NewPendingAccountWrite().SetNonce(0)

	result := paw.Merge(base, 10)

	require.Equal(t, uint64(0), result.GetNonce())
	require.Equal(t, int64(10), result.GetBlockHeight())
}

func TestPAW_ZeroBalance(t *testing.T) {
	base := NewAccountData().SetBalance(toArray32(leftPad32([]byte{0xff})))
	paw := NewPendingAccountWrite().SetBalance(&[32]byte{})

	result := paw.Merge(base, 10)

	var zero [32]byte
	require.Equal(t, &zero, result.GetBalance())
}

func TestPAW_ZeroCodeHash(t *testing.T) {
	base := NewAccountData().SetCodeHash(toArray32(bytes.Repeat([]byte{0xaa}, 32)))
	paw := NewPendingAccountWrite().SetCodeHash(&[32]byte{})

	result := paw.Merge(base, 10)

	var zero [32]byte
	require.Equal(t, &zero, result.GetCodeHash())
}

func TestPAW_ZeroAllFields_ResultIsDelete(t *testing.T) {
	base := NewAccountData().
		SetBalance(toArray32(leftPad32([]byte{0x01}))).
		SetNonce(1).
		SetCodeHash(toArray32(bytes.Repeat([]byte{0x01}, 32)))

	paw := NewPendingAccountWrite().
		SetBalance(&[32]byte{}).
		SetNonce(0).
		SetCodeHash(&[32]byte{})

	result := paw.Merge(base, 10)

	require.True(t, result.IsDelete())
}

func TestPAW_MergeDoesNotModifyBase(t *testing.T) {
	base := NewAccountData().
		SetBlockHeight(50).
		SetNonce(10)

	paw := NewPendingAccountWrite().SetNonce(99)
	_ = paw.Merge(base, 100)

	// Base must be unchanged
	require.Equal(t, int64(50), base.GetBlockHeight())
	require.Equal(t, uint64(10), base.GetNonce())
}

func TestPAW_IsSetFlags(t *testing.T) {
	paw := NewPendingAccountWrite()
	require.False(t, paw.IsBalanceSet())
	require.False(t, paw.IsNonceSet())
	require.False(t, paw.IsCodeHashSet())

	paw.SetNonce(1)
	require.False(t, paw.IsBalanceSet())
	require.True(t, paw.IsNonceSet())
	require.False(t, paw.IsCodeHashSet())

	paw.SetBalance(&[32]byte{1})
	require.True(t, paw.IsBalanceSet())

	paw.SetCodeHash(&[32]byte{2})
	require.True(t, paw.IsCodeHashSet())
}

func TestPAW_GettersReturnSetValues(t *testing.T) {
	bal := [32]byte{0xab}
	ch := [32]byte{0xcd}
	paw := NewPendingAccountWrite().
		SetBalance(&bal).
		SetNonce(123).
		SetCodeHash(&ch)

	require.Equal(t, &bal, paw.GetBalance())
	require.Equal(t, uint64(123), paw.GetNonce())
	require.Equal(t, &ch, paw.GetCodeHash())
}

func TestPAW_OverwriteField(t *testing.T) {
	paw := NewPendingAccountWrite().SetNonce(1).SetNonce(2)
	base := NewAccountData()

	result := paw.Merge(base, 10)
	require.Equal(t, uint64(2), result.GetNonce())
}

func TestPAW_ZeroThenSet(t *testing.T) {
	paw := NewPendingAccountWrite().SetNonce(0).SetNonce(42)
	base := NewAccountData().SetNonce(10)

	result := paw.Merge(base, 10)
	require.Equal(t, uint64(42), result.GetNonce())
}

func TestPAW_SetThenZero(t *testing.T) {
	paw := NewPendingAccountWrite().SetNonce(42).SetNonce(0)
	base := NewAccountData().SetNonce(10)

	result := paw.Merge(base, 10)
	require.Equal(t, uint64(0), result.GetNonce())
}

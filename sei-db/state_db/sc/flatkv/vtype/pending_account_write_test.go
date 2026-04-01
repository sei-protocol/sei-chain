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
	require.Equal(t, (*Balance)(&zero), result.GetBalance())
	require.Equal(t, (*CodeHash)(&zero), result.GetCodeHash())
}

func TestPAW_SetCodeHash_MergeOntoExistingAccount(t *testing.T) {
	base := NewAccountData().
		SetBlockHeight(50).
		SetBalance(toBalance(leftPad32([]byte{0xff}))).
		SetNonce(10).
		SetCodeHash(toCodeHash(bytes.Repeat([]byte{0xaa}, 32)))

	newCodeHash := toCodeHash(bytes.Repeat([]byte{0xbb}, 32))
	paw := NewPendingAccountWrite().SetCodeHash(newCodeHash)

	result := paw.Merge(base, 100)

	// Changed field
	require.Equal(t, newCodeHash, result.GetCodeHash())
	// Unchanged fields carried over from base
	require.Equal(t, toBalance(leftPad32([]byte{0xff})), result.GetBalance())
	require.Equal(t, uint64(10), result.GetNonce())
	// Block height updated
	require.Equal(t, int64(100), result.GetBlockHeight())
}

func TestPAW_SetBalance_MergeOntoExistingAccount(t *testing.T) {
	base := NewAccountData().
		SetBlockHeight(50).
		SetBalance(toBalance(leftPad32([]byte{0x01}))).
		SetNonce(5)

	newBalance := toBalance(leftPad32([]byte{0x02}))
	paw := NewPendingAccountWrite().SetBalance(newBalance)

	result := paw.Merge(base, 60)

	require.Equal(t, newBalance, result.GetBalance())
	require.Equal(t, uint64(5), result.GetNonce())
	require.Equal(t, int64(60), result.GetBlockHeight())
}

func TestPAW_MultipleFields(t *testing.T) {
	base := NewAccountData().
		SetBlockHeight(1).
		SetBalance(toBalance(leftPad32([]byte{0x01}))).
		SetNonce(1).
		SetCodeHash(toCodeHash(bytes.Repeat([]byte{0x01}, 32)))

	newBalance := toBalance(leftPad32([]byte{0x02}))
	newCodeHash := toCodeHash(bytes.Repeat([]byte{0x02}, 32))
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
	base := NewAccountData().SetBalance(toBalance(leftPad32([]byte{0xff})))
	var zeroBal Balance
	paw := NewPendingAccountWrite().SetBalance(&zeroBal)

	result := paw.Merge(base, 10)

	require.Equal(t, &zeroBal, result.GetBalance())
}

func TestPAW_ZeroCodeHash(t *testing.T) {
	base := NewAccountData().SetCodeHash(toCodeHash(bytes.Repeat([]byte{0xaa}, 32)))
	var zeroHash CodeHash
	paw := NewPendingAccountWrite().SetCodeHash(&zeroHash)

	result := paw.Merge(base, 10)

	require.Equal(t, &zeroHash, result.GetCodeHash())
}

func TestPAW_ZeroAllFields_ResultIsDelete(t *testing.T) {
	base := NewAccountData().
		SetBalance(toBalance(leftPad32([]byte{0x01}))).
		SetNonce(1).
		SetCodeHash(toCodeHash(bytes.Repeat([]byte{0x01}, 32)))

	var zBal Balance
	var zHash CodeHash
	paw := NewPendingAccountWrite().
		SetBalance(&zBal).
		SetNonce(0).
		SetCodeHash(&zHash)

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

	balSet := Balance{}
	balSet[0] = 1
	paw.SetBalance(&balSet)
	require.True(t, paw.IsBalanceSet())

	chSet := CodeHash{}
	chSet[0] = 2
	paw.SetCodeHash(&chSet)
	require.True(t, paw.IsCodeHashSet())
}

func TestPAW_GettersReturnSetValues(t *testing.T) {
	bal := Balance{}
	bal[0] = 0xab
	ch := CodeHash{}
	ch[0] = 0xcd
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

func TestNilPAW_Getters(t *testing.T) {
	var paw *PendingAccountWrite
	var zeroBal Balance
	var zeroHash CodeHash

	require.Equal(t, &zeroBal, paw.GetBalance())
	require.Equal(t, uint64(0), paw.GetNonce())
	require.Equal(t, &zeroHash, paw.GetCodeHash())
}

func TestNilPAW_IsSetFlags(t *testing.T) {
	var paw *PendingAccountWrite
	require.False(t, paw.IsBalanceSet())
	require.False(t, paw.IsNonceSet())
	require.False(t, paw.IsCodeHashSet())
}

func TestNilPAW_SettersAutoCreate(t *testing.T) {
	var p1 *PendingAccountWrite
	p1 = p1.SetNonce(5)
	require.NotNil(t, p1)
	require.Equal(t, uint64(5), p1.GetNonce())
	require.True(t, p1.IsNonceSet())

	var p2 *PendingAccountWrite
	bal := Balance{0x01}
	p2 = p2.SetBalance(&bal)
	require.NotNil(t, p2)
	require.Equal(t, &bal, p2.GetBalance())
	require.True(t, p2.IsBalanceSet())

	var p3 *PendingAccountWrite
	ch := CodeHash{0x02}
	p3 = p3.SetCodeHash(&ch)
	require.NotNil(t, p3)
	require.Equal(t, &ch, p3.GetCodeHash())
	require.True(t, p3.IsCodeHashSet())
}

func TestNilPAW_MergeOntoBase(t *testing.T) {
	base := NewAccountData().
		SetBlockHeight(50).
		SetNonce(10).
		SetBalance(toBalance(leftPad32([]byte{0xff})))

	var paw *PendingAccountWrite
	result := paw.Merge(base, 100)

	require.Equal(t, int64(100), result.GetBlockHeight())
	require.Equal(t, uint64(10), result.GetNonce())
	require.Equal(t, toBalance(leftPad32([]byte{0xff})), result.GetBalance())
}

func TestNilPAW_MergeOntoNilBase(t *testing.T) {
	var paw *PendingAccountWrite
	result := paw.Merge(nil, 100)

	require.NotNil(t, result)
	require.Equal(t, int64(100), result.GetBlockHeight())
	require.True(t, result.IsDelete())
}

func TestPAW_MergeOntoNilBase(t *testing.T) {
	paw := NewPendingAccountWrite().SetNonce(42)
	result := paw.Merge(nil, 100)

	require.NotNil(t, result)
	require.Equal(t, int64(100), result.GetBlockHeight())
	require.Equal(t, uint64(42), result.GetNonce())
	var zero [32]byte
	require.Equal(t, (*Balance)(&zero), result.GetBalance())
	require.Equal(t, (*CodeHash)(&zero), result.GetCodeHash())
}

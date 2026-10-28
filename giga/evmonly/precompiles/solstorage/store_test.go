package solstorage

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// mapState is the storage half of precompiles.State.
type mapState map[common.Hash]common.Hash

func (m mapState) GetState(_ common.Address, k common.Hash) common.Hash { return m[k] }
func (m mapState) SetState(_ common.Address, k, v common.Hash) {
	if v == (common.Hash{}) {
		delete(m, k)
		return
	}
	m[k] = v
}
func (m mapState) GetBalance(common.Address) *big.Int        { panic("unsupported") }
func (m mapState) AddBalance(common.Address, *big.Int)       { panic("unsupported") }
func (m mapState) SubBalance(common.Address, *big.Int) error { panic("unsupported") }
func (m mapState) GetNonce(common.Address) uint64            { panic("unsupported") }
func (m mapState) SetNonce(common.Address, uint64)           { panic("unsupported") }
func (m mapState) GetCode(common.Address) []byte             { panic("unsupported") }
func (m mapState) Failed() bool                              { return false }
func (m mapState) Snapshot() int                             { panic("unsupported") }
func (m mapState) RevertToSnapshot(int)                      { panic("unsupported") }

func TestStringShrinkingClearsChunks(t *testing.T) {
	state := mapState{}
	s := Store{State: state}
	slot := Slot(3)

	long := strings.Repeat("x", 70)
	s.SetString(slot, long)
	require.Equal(t, long, readString(t, s, slot))
	require.Len(t, state, 4, "head plus three chunks")

	s.SetString(slot, "short")
	require.Equal(t, "short", readString(t, s, slot))
	require.Len(t, state, 1, "only the inline head remains")

	s.SetString(slot, "")
	require.Empty(t, readString(t, s, slot))
	require.Empty(t, state)
}

func TestPackedFieldsLeaveNeighboursIntact(t *testing.T) {
	s := Store{State: mapState{}}
	slot := Slot(1)
	s.SetInt32(slot, 0, -2)
	s.SetBool(slot, 4, true)
	s.SetUint64(slot, 5, 1<<63+7)
	s.SetInt64(slot, 13, -9)

	require.Equal(t, int32(-2), s.Int32(slot, 0))
	require.True(t, s.Bool(slot, 4))
	require.Equal(t, uint64(1<<63+7), s.Uint64(slot, 5))
	require.Equal(t, int64(-9), s.Int64(slot, 13))

	s.SetBool(slot, 4, false)
	require.False(t, s.Bool(slot, 4))
	require.Equal(t, int32(-2), s.Int32(slot, 0))
	require.Equal(t, uint64(1<<63+7), s.Uint64(slot, 5))
}

func TestSetSwapAndPop(t *testing.T) {
	state := mapState{}
	set := Set{Store: Store{State: state}, Base: Slot(9)}
	for i := uint64(1); i <= 3; i++ {
		require.True(t, set.Add(Slot(i)))
	}
	require.False(t, set.Add(Slot(2)))

	require.True(t, set.Remove(Slot(1)))
	require.False(t, set.Remove(Slot(1)))
	require.Equal(t, uint64(2), set.Len())
	require.Equal(t, Slot(3), set.At(0), "the last member moves into the removed position")
	require.Equal(t, Slot(2), set.At(1))
	require.True(t, set.Contains(Slot(3)))

	require.True(t, set.Remove(Slot(3)))
	require.True(t, set.Remove(Slot(2)))
	require.Empty(t, state, "an emptied set leaves no storage behind")
}

func TestSetRemoveIgnoresAPositionPastItsLength(t *testing.T) {
	state := mapState{}
	set := Set{Store: Store{State: state}, Base: Slot(9)}
	require.True(t, set.Add(Slot(1)))
	set.Store.SetWord(set.Base, Slot(0)) // a length read back as zero, as after a failed read
	require.False(t, set.Remove(Slot(1)))
	require.Equal(t, uint64(0), set.Len(), "no wrapped length is written")
}

func TestOffsetWraps(t *testing.T) {
	max := common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	require.Equal(t, Slot(1), Offset(max, 2))
}

// failingState fails every state access after the failAt-th, as running out of
// gas does: reads return zero and writes are dropped. With failAt 0 it never
// fails.
type failingState struct {
	mapState
	accesses, failAt, accessesAfterFailure int
}

func (f *failingState) access() bool {
	f.accesses++
	if f.Failed() {
		f.accessesAfterFailure++
		return false
	}
	return true
}

func (f *failingState) GetState(a common.Address, k common.Hash) common.Hash {
	if !f.access() {
		return common.Hash{}
	}
	return f.mapState.GetState(a, k)
}

func (f *failingState) SetState(a common.Address, k, v common.Hash) {
	if f.access() {
		f.mapState.SetState(a, k, v)
	}
}

func (f *failingState) Failed() bool { return f.failAt > 0 && f.accesses > f.failAt }

func TestStringStopsOnceReadsFail(t *testing.T) {
	state := &failingState{mapState: mapState{}}
	s := Store{State: state}
	s.SetString(Slot(3), strings.Repeat("x", 32*100))
	state.accesses, state.failAt = 0, 5
	readString(t, s, Slot(3))
	require.Equal(t, 1, state.accessesAfterFailure, "the chunk loop stops at the first failed read")
}

func TestSetStringZeroingStopsOnceWritesFail(t *testing.T) {
	state := &failingState{mapState: mapState{}}
	s := Store{State: state}
	s.SetString(Slot(3), strings.Repeat("x", 32*100))
	state.accesses, state.failAt = 0, 3
	s.SetString(Slot(3), "short")
	require.Equal(t, 2, state.accessesAfterFailure, "the zeroing loop stops at the first failed write; the head write follows")
}

func TestInvalidStoredStringIsAnError(t *testing.T) {
	state := mapState{}
	s := Store{State: state}
	require.ErrorIs(t, s.SetString(Slot(3), strings.Repeat("x", MaxStringLength+1)), ErrInvalidString)
	for name, head := range map[string]common.Hash{
		"long past the ceiling": common.BigToHash(new(big.Int).SetUint64((MaxStringLength+1)*2 + 1)),
		"long held inline":      common.BigToHash(big.NewInt(31*2 + 1)),
		"short past one word":   {31: 32 * 2},
	} {
		state[Slot(3)] = head
		_, err := s.String(Slot(3))
		require.ErrorIs(t, err, ErrInvalidString, name)
		_, err = s.StringChunks(Slot(3))
		require.ErrorIs(t, err, ErrInvalidString, name)
	}
	s.SetString(Slot(4), strings.Repeat("x", MaxStringLength))
	require.Len(t, readString(t, s, Slot(4)), MaxStringLength)
}

func readString(t *testing.T, s Store, slot common.Hash) string {
	t.Helper()
	v, err := s.String(slot)
	require.NoError(t, err)
	return v
}

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

func TestStringShrinkingClearsChunks(t *testing.T) {
	state := mapState{}
	s := Store{State: state}
	slot := Slot(3)

	long := strings.Repeat("x", 70)
	s.SetString(slot, long)
	require.Equal(t, long, s.String(slot))
	require.Len(t, state, 4, "head plus three chunks")

	s.SetString(slot, "short")
	require.Equal(t, "short", s.String(slot))
	require.Len(t, state, 1, "only the inline head remains")

	s.SetString(slot, "")
	require.Empty(t, s.String(slot))
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

func TestOffsetWraps(t *testing.T) {
	max := common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	require.Equal(t, Slot(1), Offset(max, 2))
}

package bud

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBUDTreeRejectsUnsortedBudlets(t *testing.T) {
	testCases := map[string][]*Budlet{
		"unsorted": {
			newTestBudlet(t, "evm/b", []byte{0x01}, []byte{}, 0),
			newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 0),
		},
		"duplicate key": {
			newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 0),
			newTestBudlet(t, "evm/a", []byte{0x02}, []byte{0x01}, 0),
		},
	}
	for name, budlets := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := ComputeBUDTreeHash(budlets)
			require.Error(t, err)
			_, _, err = NewBUDProof(budlets, []byte("evm/a"))
			require.Error(t, err)
		})
	}
}

func TestBUDTreeRejectsNilAndInvalidBudlets(t *testing.T) {
	valid := newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 0)
	testCases := map[string][]*Budlet{
		"nil budlet":          {nil},
		"nil after a budlet":  {valid, nil},
		"zero budlet":         {&Budlet{}},
		"zero after a budlet": {valid, &Budlet{}},
	}
	for name, budlets := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := ComputeBUDTreeHash(budlets)
			require.Error(t, err)
			_, _, err = NewBUDProof(budlets, []byte("evm/a"))
			require.Error(t, err)
		})
	}
}

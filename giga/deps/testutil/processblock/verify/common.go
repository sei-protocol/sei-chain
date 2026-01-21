package verify

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
)

type BlockRunnable func() (resultCodes []uint32)

type Verifier func(*testing.T, *processblock.App, BlockRunnable, []signing.Tx) BlockRunnable

// inefficient so only for test
func removeMatched[T any](l []T, matcher func(T) bool) []T {
	newL := []T{}
	for _, i := range l {
		if !matcher(i) {
			newL = append(newL, i)
		}
	}
	return newL
}

package factory

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestMakeHeader(t *testing.T) {
	MakeHeader(&types.Header{})
}

func TestRandomNodeID(t *testing.T) {
	RandomNodeID(t)
}

package factory

import (
	"testing"

	"github.com/tendermint/tendermint/types"
)

func TestMakeHeader(t *testing.T) {
	MakeHeader(&types.Header{})
}

func TestRandomNodeID(t *testing.T) {
	RandomNodeID(t)
}

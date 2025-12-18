package tracks_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/giga/executor/tracks"
	"github.com/tendermint/tendermint/libs/utils/require"
)

type testBlock struct{ id uint64 }

func (b *testBlock) GetID() uint64 { return b.id }

func TestStatelessTrack(t *testing.T) {
	inputs := make(chan *testBlock, 100)
	outputs := make(chan *testBlock, 100)
	processFn := func(input *testBlock) *testBlock {
		sleepDuration := time.Duration(rand.Intn(100)) * time.Millisecond
		time.Sleep(sleepDuration)
		return &testBlock{id: input.GetID()}
	}
	workerCount := 10
	lastBlock := uint64(100)
	tracks.StartStatelessTrack(inputs, outputs, processFn, workerCount, lastBlock)
	for i := range 100 {
		inputs <- &testBlock{id: uint64(i) + 1 + lastBlock}
	}
	for i := range 100 {
		output := <-outputs
		require.Equal(t, uint64(i)+1+lastBlock, output.GetID())
	}
}

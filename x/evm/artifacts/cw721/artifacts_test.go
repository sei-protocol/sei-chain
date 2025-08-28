package cw721_test

import (
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/stretchr/testify/require"
)

// run with `-race`
func TestGetBinConcurrent(t *testing.T) {
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			require.NotEmpty(t, cw721.GetBin())
		}(i)
	}

	wg.Wait()
}

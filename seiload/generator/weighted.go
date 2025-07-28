package generator

import (
	"context"
	"github.com/sei-protocol/sei-chain/seiload/types"
	"math/rand"
	"sync"
)

// WeightedCfg is a configuration for a weighted scenarioGenerator.
type WeightedCfg struct {
	Weight    int
	Generator Generator
}

// WeightedConfig creates a configuration for a weighted scenarioGenerator.
func WeightedConfig(weight int, generator Generator) *WeightedCfg {
	return &WeightedCfg{
		Weight:    weight,
		Generator: generator,
	}
}

type weightedGenerator struct {
	generators []Generator
	mx         sync.Mutex
	counter    int64
}

// GenerateInfinite generates transactions indefinitely.
func (w *weightedGenerator) GenerateInfinite(ctx context.Context) <-chan *types.LoadTx {
	output := make(chan *types.LoadTx, 10000)
	go func() {
		defer close(output)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				select {
				case <-ctx.Done():
					return
				case output <- w.nextGenerator().Generate():
				}
			}
		}
	}()
	return output
}

func (w *weightedGenerator) nextIndex() int64 {
	w.mx.Lock()
	defer w.mx.Unlock()
	w.counter++
	if w.counter >= int64(len(w.generators)) {
		w.counter = 0
	}
	return w.counter
}

// generators are randomized at startup.
func (w *weightedGenerator) nextGenerator() Generator {
	return w.generators[w.nextIndex()]
}

// GenerateN generates n transactions.
func (w *weightedGenerator) GenerateN(n int) []*types.LoadTx {
	txs := make([]*types.LoadTx, 0, n)
	for range n {
		txs = append(txs, w.Generate())
	}
	return txs
}

// Generate generates 1 transaction.
func (w *weightedGenerator) Generate() *types.LoadTx {
	return w.nextGenerator().Generate()
}

// NewWeightedGenerator creates a new scenarioGenerator that will randomly select from the provided generators.
func NewWeightedGenerator(cfgs ...*WeightedCfg) Generator {
	// no need for clever weighting logic if we just have 1 scenarioGenerator anyway.
	if len(cfgs) == 1 {
		return cfgs[0].Generator
	}
	var weighted []Generator
	for _, cfg := range cfgs {
		for range cfg.Weight {
			weighted = append(weighted, cfg.Generator)
		}
	}

	rand.Shuffle(len(weighted), func(i, j int) {
		weighted[i], weighted[j] = weighted[j], weighted[i]
	})

	return &weightedGenerator{
		generators: weighted,
	}
}

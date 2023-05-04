package sync

import (
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type GasWrapper struct {
	sdk.GasMeter
	mu *sync.Mutex
}

func NewGasWrapper(wrapped sdk.GasMeter) sdk.GasMeter {
	return GasWrapper{GasMeter: wrapped, mu: &sync.Mutex{}}
}

func (g GasWrapper) ConsumeGas(amount sdk.Gas, descriptor string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.GasMeter.ConsumeGas(amount, descriptor)
}

func (g GasWrapper) RefundGas(amount sdk.Gas, descriptor string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.GasMeter.RefundGas(amount, descriptor)
}

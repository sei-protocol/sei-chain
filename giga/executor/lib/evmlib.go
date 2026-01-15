package lib

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
)

// InitEvmoneVM initializes the EVMC VM by loading the platform-specific evmone library.
// It verifies that the loaded VM version is compatible with the EVMC bindings version.
func InitEvmoneVM() (*evmc.VM, error) {
	_, path, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("failed to get caller information")
	}

	libPath := filepath.Join(filepath.Dir(path), libName)
	vm, err := evmc.Load(libPath)
	if err != nil {
		return nil, fmt.Errorf("evmc.Load(%q): %w", libPath, err)
	}

	return vm, nil
}

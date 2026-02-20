package lib

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
)

//go:generate go run ./gen/main.go .

// InitEvmoneVM initializes the EVMC VM by loading the platform-specific evmone library.
// It does not verify that the loaded version is compatible with evmc version.
// If GIGA_EVMONE_LIB_DIR is set, the library is loaded from that directory (for containers
// where the build-time path from runtime.Caller is wrong).
func InitEvmoneVM() (*evmc.VM, error) {
	var libPath string
	if dir := os.Getenv("GIGA_EVMONE_LIB_DIR"); dir != "" {
		libPath = filepath.Join(dir, libName)
	} else {
		_, path, _, ok := runtime.Caller(0)
		if !ok {
			return nil, fmt.Errorf("failed to get caller information")
		}
		libPath = filepath.Join(filepath.Dir(path), libName)
	}
	vm, err := evmc.Load(libPath)
	if err != nil {
		return nil, fmt.Errorf("evmc.Load(%q): %w", libPath, err)
	}

	return vm, nil
}

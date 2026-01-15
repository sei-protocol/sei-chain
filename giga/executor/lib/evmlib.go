package lib

import (
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

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

	if err := verifyVersionCompatibility(vm); err != nil {
		return nil, err
	}

	return vm, nil
}

// verifyVersion checks that the loaded evmone version is compatible with the EVMC bindings.
// EVMC v12.x.x requires evmone 0.12.x
func verifyVersionCompatibility(vm *evmc.VM) error {
	expectedPrefix, err := getExpectedEvmoneVersionPrefix()
	if err != nil {
		return fmt.Errorf("failed to determine expected evmone version: %w", err)
	}

	actualVersion := vm.Version()
	if !strings.HasPrefix(actualVersion, expectedPrefix) {
		return fmt.Errorf("evmone version mismatch: got %s, want %s.x (based on EVMC dependency)", actualVersion, expectedPrefix)
	}

	return nil
}

// getExpectedEvmoneVersionPrefix derives the expected evmone version prefix from the EVMC module version.
// EVMC v12.x.x -> evmone 0.12
// EVMC v11.x.x -> evmone 0.11
func getExpectedEvmoneVersionPrefix() (string, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", fmt.Errorf("failed to read build info")
	}

	for _, dep := range info.Deps {
		if strings.HasPrefix(dep.Path, "github.com/ethereum/evmc/") {
			// Version is like "v12.1.0" - extract major version (12)
			version := strings.TrimPrefix(dep.Version, "v")
			parts := strings.Split(version, ".")
			if len(parts) < 1 {
				return "", fmt.Errorf("invalid EVMC version format: %s", dep.Version)
			}
			// EVMC v12 -> evmone 0.12
			return "0." + parts[0], nil
		}
	}

	return "", fmt.Errorf("EVMC module not found in build info")
}

//go:build !((linux && amd64) || (linux && arm64) || (darwin && arm64))

package lib

// evmone is not available for this platform.
// Supported platforms: linux/amd64, linux/arm64, darwin/arm64
//
// If you see a compile error referencing this file, you are building
// for an unsupported OS/architecture combination.
const libName = evmone_unsupported_platform__only_linux_amd64_linux_arm64_and_darwin_arm64_are_supported
